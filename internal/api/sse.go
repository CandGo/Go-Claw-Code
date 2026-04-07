package api

import "strings"

// SseEvent represents a single parsed Server-Sent Event.
type SseEvent struct {
	Event string // event name (may be empty)
	Data  string // joined data lines
	ID    string // last event ID
	Retry int    // retry interval in ms (0 = unset)
}

// IncrementalSseParser incrementally parses an SSE byte stream.
// Push chunks as they arrive and collect fully formed events after each push.
type IncrementalSseParser struct {
	buffer    string
	eventName string
	dataLines []string
	id        string
	retry     int
	hasRetry  bool
}

// NewIncrementalSseParser creates a new incremental SSE parser.
func NewIncrementalSseParser() *IncrementalSseParser {
	return &IncrementalSseParser{}
}

// PushChunk feeds a new chunk of text into the parser and returns
// any complete events formed by lines ending in this chunk.
func (p *IncrementalSseParser) PushChunk(chunk string) []SseEvent {
	p.buffer += chunk
	var events []SseEvent

	for {
		idx := strings.IndexByte(p.buffer, '\n')
		if idx < 0 {
			break
		}
		line := p.buffer[:idx]
		p.buffer = p.buffer[idx+1:]
		// Trim trailing \r if present
		line = strings.TrimRight(line, "\r")
		p.processLine(line, &events)
	}

	return events
}

// Finish flushes any remaining buffered data and returns the final events.
func (p *IncrementalSseParser) Finish() []SseEvent {
	var events []SseEvent
	if p.buffer != "" {
		line := strings.TrimRight(p.buffer, "\r")
		p.processLine(line, &events)
		p.buffer = ""
	}
	if evt, ok := p.takeEvent(); ok {
		events = append(events, evt)
	}
	return events
}

func (p *IncrementalSseParser) processLine(line string, events *[]SseEvent) {
	if line == "" {
		if evt, ok := p.takeEvent(); ok {
			*events = append(*events, evt)
		}
		return
	}

	// Comment lines start with ':'
	if strings.HasPrefix(line, ":") {
		return
	}

	// Split field and value
	var field, value string
	if idx := strings.IndexByte(line, ':'); idx >= 0 {
		field = line[:idx]
		value = line[idx+1:]
		// SSE spec: strip a single leading space after the colon
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
	} else {
		field = line
	}

	switch field {
	case "event":
		p.eventName = value
	case "data":
		p.dataLines = append(p.dataLines, value)
	case "id":
		p.id = value
	case "retry":
		var n int
		for _, ch := range value {
			if ch >= '0' && ch <= '9' {
				n = n*10 + int(ch-'0')
			} else {
				return // invalid retry, ignore
			}
		}
		p.retry = n
		p.hasRetry = true
	}
}

func (p *IncrementalSseParser) takeEvent() (SseEvent, bool) {
	if len(p.dataLines) == 0 && p.eventName == "" && p.id == "" && !p.hasRetry {
		return SseEvent{}, false
	}

	data := strings.Join(p.dataLines, "\n")
	evt := SseEvent{
		Event: p.eventName,
		Data:  data,
		ID:    p.id,
	}
	if p.hasRetry {
		evt.Retry = p.retry
	}

	// Reset accumulator state
	p.eventName = ""
	p.dataLines = nil
	p.id = ""
	p.retry = 0
	p.hasRetry = false

	return evt, true
}
