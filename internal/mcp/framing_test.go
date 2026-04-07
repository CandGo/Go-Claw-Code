package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestContentLengthFraming tests the Content-Length: N\r\n\r\n{json} framing format.
func TestContentLengthFraming(t *testing.T) {
	// Simulate a JSON-RPC response with Content-Length framing
	payload := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{}}}`
	frame := "Content-Length: " + itoa(len(payload)) + "\r\n\r\n" + payload

	// Verify we can parse the frame
	reader := bufio.NewReader(strings.NewReader(frame))
	// Read header line
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read header: %v", err)
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "Content-Length:") {
		t.Errorf("expected Content-Length header, got: %s", line)
	}
	lengthStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
	if lengthStr != itoa(len(payload)) {
		t.Errorf("Content-Length = %q, want %d", lengthStr, len(payload))
	}

	// Read empty separator line
	sep, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read separator: %v", err)
	}
	if strings.TrimRight(sep, "\r\n") != "" {
		t.Errorf("expected empty line after header, got: %q", sep)
	}

	// Read payload
	buf := make([]byte, len(payload))
	if _, err := reader.Read(buf); err != nil {
		t.Fatalf("failed to read payload: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(buf, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", result["jsonrpc"])
	}
}

// TestJSONRPCRequestFormat tests that JSON-RPC requests have the correct structure.
func TestJSONRPCRequestFormat(t *testing.T) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "claw",
				"version": "0.5.0",
			},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if parsed["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", parsed["jsonrpc"])
	}
	if parsed["method"] != "initialize" {
		t.Errorf("method = %v, want initialize", parsed["method"])
	}
	if _, ok := parsed["params"]; !ok {
		t.Error("expected params field")
	}
}

// TestJSONRPCResponseParsing tests parsing of JSON-RPC responses.
func TestJSONRPCResponseParsing(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"test-server","version":"1.0.0"}}}`
	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", resp.JSONRPC)
	}
	if resp.ID != 1 {
		t.Errorf("id = %d, want 1", resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("expected no error, got: %+v", resp.Error)
	}
}

// TestJSONRPCErrorResponse tests parsing of JSON-RPC error responses.
func TestJSONRPCErrorResponse(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"Invalid Request"}}`
	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != -32600 {
		t.Errorf("error code = %d, want -32600", resp.Error.Code)
	}
	if resp.Error.Message != "Invalid Request" {
		t.Errorf("error message = %q, want 'Invalid Request'", resp.Error.Message)
	}
}

// TestJSONRPCNotification tests that notifications have no ID.
func TestJSONRPCNotification(t *testing.T) {
	notif := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  "notifications/cancelled",
		Params: map[string]interface{}{
			"requestId": 5,
			"reason":    "timeout",
		},
	}
	data, err := json.Marshal(notif)
	if err != nil {
		t.Fatalf("failed to marshal notification: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if _, hasID := parsed["id"]; hasID {
		t.Error("notification should not have id field")
	}
	if parsed["method"] != "notifications/cancelled" {
		t.Errorf("method = %v, want notifications/cancelled", parsed["method"])
	}
}

// TestContentLengthRoundTrip tests writing and reading Content-Length framed messages.
func TestContentLengthRoundTrip(t *testing.T) {
	// Build a framed message
	payload := `{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"Read","description":"Read a file"}]}}`
	var buf bytes.Buffer
	buf.WriteString("Content-Length: ")
	buf.WriteString(itoa(len(payload)))
	buf.WriteString("\r\n\r\n")
	buf.WriteString(payload)

	// Parse it back
	reader := bufio.NewReader(&buf)
	line, _ := reader.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	var contentLength int
	if strings.HasPrefix(line, "Content-Length:") {
		val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
		for _, c := range val {
			if c >= '0' && c <= '9' {
				contentLength = contentLength*10 + int(c-'0')
			}
		}
	}
	// Read separator
	reader.ReadString('\n')
	// Read payload
	data := make([]byte, contentLength)
	reader.Read(data)

	if string(data) != payload {
		t.Errorf("round-trip failed:\ngot:  %s\nwant: %s", string(data), payload)
	}
}

// TestContentLengthMultipleMessages tests reading multiple framed messages.
func TestContentLengthMultipleMessages(t *testing.T) {
	msg1 := `{"jsonrpc":"2.0","id":1,"result":{}}`
	msg2 := `{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`

	var buf bytes.Buffer
	for _, msg := range []string{msg1, msg2} {
		buf.WriteString("Content-Length: ")
		buf.WriteString(itoa(len(msg)))
		buf.WriteString("\r\n\r\n")
		buf.WriteString(msg)
	}

	reader := bufio.NewReader(&buf)
	for i, expected := range []string{msg1, msg2} {
		line, _ := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		var length int
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			for _, c := range val {
				if c >= '0' && c <= '9' {
					length = length*10 + int(c-'0')
				}
			}
		}
		reader.ReadString('\n') // separator
		data := make([]byte, length)
		reader.Read(data)
		if string(data) != expected {
			t.Errorf("message %d round-trip failed:\ngot:  %s\nwant: %s", i+1, string(data), expected)
		}
	}
}

// --- Helpers ---

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
