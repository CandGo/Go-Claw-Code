// Package voice provides audio capture, transcription, and encoding utilities.
package voice

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CaptureConfig controls audio recording parameters.
type CaptureConfig struct {
	SampleRate  int
	Channels    int
	MaxDuration time.Duration
}

// DefaultCaptureConfig returns sensible defaults for speech recording.
func DefaultCaptureConfig() CaptureConfig {
	return CaptureConfig{
		SampleRate:  16000,
		Channels:    1,
		MaxDuration: 60 * time.Second,
	}
}

// AudioCapture records raw PCM audio from the system microphone.
type AudioCapture struct {
	cfg    CaptureConfig
	cmd    *exec.Cmd
	stdout io.Reader
	mu     sync.Mutex
	active bool
}

// NewCapture creates a new audio capture device.
func NewCapture(cfg CaptureConfig) *AudioCapture {
	return &AudioCapture{cfg: cfg}
}

// Begin starts recording. It probes for available backends in order:
// SoX rec > ALSA arecord > FFmpeg.
func (ac *AudioCapture) Begin(ctx context.Context) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.active {
		return fmt.Errorf("capture already in progress")
	}

	backend, args := ac.pickBackend(ctx)
	if backend == "" {
		return fmt.Errorf("no audio capture backend found (install sox, alsa-utils, or ffmpeg)")
	}

	cmd := exec.CommandContext(ctx, backend, args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe creation failed: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", backend, err)
	}

	ac.cmd = cmd
	ac.stdout = pipe
	ac.active = true
	return nil
}

// Finish stops recording and returns captured PCM data.
func (ac *AudioCapture) Finish() ([]byte, error) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if !ac.active || ac.cmd == nil || ac.cmd.Process == nil {
		return nil, fmt.Errorf("not currently capturing")
	}
	ac.cmd.Process.Kill()

	data, err := io.ReadAll(ac.stdout)
	if err != nil {
		return nil, fmt.Errorf("failed reading audio: %w", err)
	}

	ac.active = false
	ac.cmd = nil
	return data, nil
}

// Active reports whether a capture is in progress.
func (ac *AudioCapture) Active() bool {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.active
}

func (ac *AudioCapture) pickBackend(_ context.Context) (string, []string) {
	sr := fmt.Sprintf("%d", ac.cfg.SampleRate)
	ch := fmt.Sprintf("%d", ac.cfg.Channels)

	if _, err := exec.LookPath("rec"); err == nil {
		return "rec", []string{"-t", "raw", "-r", sr, "-e", "signed-integer", "-b", "16", "-c", ch, "-"}
	}
	if _, err := exec.LookPath("arecord"); err == nil {
		return "arecord", []string{"-f", "S16_LE", "-r", sr, "-c", ch, "-t", "raw", "-"}
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		return "ffmpeg", []string{"-f", "alsa", "-i", "default", "-f", "s16le", "-ac", ch, "-ar", sr, "-"}
	}
	return "", nil
}

// Transcriber converts raw audio bytes into text.
type Transcriber interface {
	Transcribe(ctx context.Context, pcmData []byte) (string, error)
}

// StubTranscriber returns a fixed response. Useful for testing.
type StubTranscriber struct {
	Text string
}

func (s *StubTranscriber) Transcribe(_ context.Context, _ []byte) (string, error) {
	return s.Text, nil
}

// VoiceInput combines capture and transcription into a single workflow.
type VoiceInput struct {
	capture     *AudioCapture
	transcriber Transcriber
	mu          sync.Mutex
	on          bool
}

// NewVoiceInput creates a ready-to-use voice input service.
func NewVoiceInput(cfg CaptureConfig, t Transcriber) *VoiceInput {
	return &VoiceInput{
		capture:     NewCapture(cfg),
		transcriber: t,
	}
}

// Enable / Disable control whether voice input is available.
func (v *VoiceInput) Enable()  { v.mu.Lock(); v.on = true; v.mu.Unlock() }
func (v *VoiceInput) Disable() { v.mu.Lock(); v.on = false; v.mu.Unlock() }
func (v *VoiceInput) Enabled() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.on
}

// RecordAndTranscribe captures audio until stopped, then returns the transcribed text.
func (v *VoiceInput) RecordAndTranscribe(ctx context.Context) (string, error) {
	if !v.Enabled() {
		return "", fmt.Errorf("voice input is disabled")
	}
	if err := v.capture.Begin(ctx); err != nil {
		return "", err
	}
	pcm, err := v.capture.Finish()
	if err != nil {
		return "", err
	}
	if len(pcm) == 0 {
		return "", fmt.Errorf("captured zero audio samples")
	}
	return v.transcriber.Transcribe(ctx, pcm)
}

// CaptureActive reports whether currently recording.
func (v *VoiceInput) CaptureActive() bool { return v.capture.Active() }

// WAV converts raw signed-16-bit PCM data into a complete WAV file.
func WAV(pcm []byte, sampleRate, channels int) []byte {
	dataSize := len(pcm)
	byteRate := sampleRate * channels * 2
	blockAlign := channels * 2

	buf := make([]byte, 44+len(pcm))

	// RIFF header
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")

	// fmt chunk
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)           // chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)            // PCM
	binary.LittleEndian.PutUint16(buf[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], 16)           // bits per sample

	// data chunk
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))
	copy(buf[44:], pcm)

	return buf
}

// Intent represents a recognized voice command.
type Intent struct {
	Verb   string
	Object string
	Raw    string
}

// ParseIntent does lightweight keyword extraction on transcribed text.
func ParseIntent(text string) Intent {
	lower := strings.ToLower(text)
	words := strings.Fields(lower)

	verbs := map[string]string{
		"go to": "navigate", "open": "navigate",
		"delete": "delete", "remove": "delete",
		"search": "search", "find": "search",
		"copy": "copy", "paste": "paste",
		"undo": "undo", "redo": "redo",
	}

	for phrase, verb := range verbs {
		if strings.Contains(lower, phrase) {
			obj := extractTail(words, phrase)
			return Intent{Verb: verb, Object: obj, Raw: text}
		}
	}
	return Intent{Verb: "input", Object: text, Raw: text}
}

func extractTail(words []string, trigger string) string {
	parts := strings.Fields(trigger)
	tailStart := -1
	for i := 0; i <= len(words)-len(parts); i++ {
		match := true
		for j, p := range parts {
			if words[i+j] != p {
				match = false
				break
			}
		}
		if match {
			tailStart = i + len(parts)
			break
		}
	}
	if tailStart >= 0 && tailStart < len(words) {
		return strings.Join(words[tailStart:], " ")
	}
	return ""
}
