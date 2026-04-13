package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
)

// WhisperTranscriber sends audio to the Whisper API for transcription.
type WhisperTranscriber struct {
	APIKey   string
	Model    string // default: "whisper-1"
	Endpoint string // default: "https://api.openai.com/v1/audio/transcriptions"
	Language string // optional language hint (e.g. "en", "zh")
}

// NewWhisperTranscriber creates a transcriber using the OpenAI Whisper API.
func NewWhisperTranscriber(apiKey string) *WhisperTranscriber {
	return &WhisperTranscriber{
		APIKey:   apiKey,
		Model:    "whisper-1",
		Endpoint: "https://api.openai.com/v1/audio/transcriptions",
	}
}

// Transcribe converts PCM audio to text via the Whisper API.
func (t *WhisperTranscriber) Transcribe(ctx context.Context, pcmData []byte) (string, error) {
	if t.APIKey == "" {
		return "", fmt.Errorf("whisper API key not configured")
	}

	// Convert PCM to WAV
	wavData := WAV(pcmData, 16000, 1)

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Write model field
	if err := writer.WriteField("model", t.Model); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}

	// Write language field if specified
	if t.Language != "" {
		if err := writer.WriteField("language", t.Language); err != nil {
			return "", fmt.Errorf("failed to write language field: %w", err)
		}
	}

	// Write audio file part
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(wavData); err != nil {
		return "", fmt.Errorf("failed to write audio data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", t.Endpoint, &buf)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.APIKey)

	// Send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisper API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("whisper API error (%d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Text, nil
}

// WhisperConfigFromEnv creates a WhisperTranscriber from environment variables.
// Checks OPENAI_API_KEY first, then CLAW_WHISPER_KEY.
func WhisperConfigFromEnv() *WhisperTranscriber {
	apiKey := os.Getenv("CLAW_WHISPER_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return nil
	}

	t := NewWhisperTranscriber(apiKey)

	// Allow custom endpoint for self-hosted Whisper
	if endpoint := os.Getenv("CLAW_WHISPER_ENDPOINT"); endpoint != "" {
		t.Endpoint = endpoint
	}
	if model := os.Getenv("CLAW_WHISPER_MODEL"); model != "" {
		t.Model = model
	}
	if lang := os.Getenv("CLAW_WHISPER_LANGUAGE"); lang != "" {
		t.Language = lang
	}

	return t
}

// MockWhisperServer returns a fake transcription for testing.
type MockWhisperServer struct {
	Response string
}

// Transcribe returns the mock response.
func (m *MockWhisperServer) Transcribe(_ context.Context, _ []byte) (string, error) {
	return m.Response, nil
}
