package api

import (
	"bufio"
	"io"
	"strings"
)

// SSEFrame represents a single Server-Sent Event frame.
type SSEFrame struct {
	Event string
	Data  string
}

// ParseSSEStream reads SSE frames from an io.Reader and sends them to a channel.
func ParseSSEStream(r io.Reader) <-chan SSEFrame {
	ch := make(chan SSEFrame, 64)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var event, data strings.Builder
		hasContent := false

		for scanner.Scan() {
			line := scanner.Text()

			if line == "" {
				if hasContent {
					ch <- SSEFrame{
						Event: strings.TrimSpace(event.String()),
						Data:  data.String(),
					}
					event.Reset()
					data.Reset()
					hasContent = false
				}
				continue
			}

			if strings.HasPrefix(line, "event:") {
				event.WriteString(strings.TrimPrefix(line, "event:"))
				hasContent = true
			} else if strings.HasPrefix(line, "data:") {
				if data.Len() > 0 {
					data.WriteString("\n")
				}
				data.WriteString(strings.TrimPrefix(line, "data:"))
				hasContent = true
			}
		}
	}()
	return ch
}
