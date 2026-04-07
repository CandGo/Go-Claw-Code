package api

import (
	"bytes"
	"io"
	"strings"
)

// SSEFrame represents a single Server-Sent Event frame.
type SSEFrame struct {
	Event string
	Data  string
}

// ParseSSEStream reads SSE frames from an io.Reader using chunk-based incremental parsing.
// Mirrors Rust's SseParser: accumulates raw bytes and extracts complete frames by finding
// the \n\n separator, rather than using line-based bufio.Scanner.
func ParseSSEStream(r io.Reader) <-chan SSEFrame {
	ch := make(chan SSEFrame, 64)
	go func() {
		defer close(ch)

		var buf bytes.Buffer
		tmp := make([]byte, 32*1024)

		for {
			n, readErr := r.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}

			// Extract all complete frames from the buffer
			for {
				frame := extractNextFrame(&buf)
				if frame == "" {
					break
				}
				eventName, data := parseSSEFrameContent(frame)
				if data == "" || data == "[DONE]" {
					continue
				}
				ch <- SSEFrame{Event: eventName, Data: data}
			}

			if readErr != nil {
				// Drain any trailing bytes (mirrors Rust SseParser::finish)
				if buf.Len() > 0 {
					trailing := strings.TrimRight(buf.String(), " \t\r\n")
					if trailing != "" {
						eventName, data := parseSSEFrameContent(trailing)
						if data != "" && data != "[DONE]" {
							ch <- SSEFrame{Event: eventName, Data: data}
						}
					}
				}
				return
			}
		}
	}()
	return ch
}

// extractNextFrame extracts one SSE frame from the buffer.
// Looks for \n\n or \r\n\r\n as the frame separator.
// Returns the frame content (without the separator), or empty string if no complete frame.
// Mirrors Rust SseParser::next_frame.
func extractNextFrame(buf *bytes.Buffer) string {
	b := buf.Bytes()

	// Look for \n\n separator
	sep := bytes.Index(b, []byte("\n\n"))
	sepLen := 2
	if sep == -1 {
		// Try \r\n\r\n
		sep = bytes.Index(b, []byte("\r\n\r\n"))
		sepLen = 4
	}

	if sep == -1 {
		return ""
	}

	frame := string(b[:sep])
	// Advance past the separator
	buf.Next(sep + sepLen)
	return frame
}

// parseSSEFrameContent parses a single SSE frame string into event name and data.
// Mirrors Rust parse_frame: iterates lines, extracts event:/data: prefixes,
// skips comment lines (starting with :), trims leading whitespace from data values.
func parseSSEFrameContent(frame string) (eventName string, data string) {
	var dataLines []string

	for _, line := range strings.Split(frame, "\n") {
		line = strings.TrimRight(line, "\r")

		// Skip comment lines
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if strings.HasPrefix(line, "data:") {
			// Trim leading whitespace after data: (mirrors Rust's data.trim_start())
			value := strings.TrimLeft(strings.TrimPrefix(line, "data:"), " ")
			dataLines = append(dataLines, value)
		}
	}

	if len(dataLines) == 0 {
		return eventName, ""
	}

	return eventName, strings.Join(dataLines, "\n")
}
