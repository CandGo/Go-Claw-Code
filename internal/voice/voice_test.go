package voice

import (
	"context"
	"testing"
)

func TestDefaultCaptureConfig(t *testing.T) {
	cfg := DefaultCaptureConfig()
	if cfg.SampleRate != 16000 {
		t.Errorf("SampleRate = %d, want 16000", cfg.SampleRate)
	}
	if cfg.Channels != 1 {
		t.Errorf("Channels = %d, want 1", cfg.Channels)
	}
	if cfg.MaxDuration.Seconds() != 60 {
		t.Errorf("MaxDuration = %v, want 60s", cfg.MaxDuration)
	}
}

func TestWAV_HeaderStructure(t *testing.T) {
	pcm := make([]byte, 16000*2) // 1 second of 16kHz 16-bit mono
	wav := WAV(pcm, 16000, 1)

	if string(wav[0:4]) != "RIFF" {
		t.Error("missing RIFF magic")
	}
	if string(wav[8:12]) != "WAVE" {
		t.Error("missing WAVE format")
	}
	if string(wav[12:16]) != "fmt " {
		t.Error("missing fmt chunk")
	}
	if string(wav[36:40]) != "data" {
		t.Error("missing data chunk")
	}

	// Data size should match PCM length
	dataSize := uint32(wav[40]) | uint32(wav[41])<<8 | uint32(wav[42])<<16 | uint32(wav[43])<<24
	if int(dataSize) != len(pcm) {
		t.Errorf("data chunk size = %d, want %d", dataSize, len(pcm))
	}

	// Total file should be header + PCM
	if len(wav) != 44+len(pcm) {
		t.Errorf("WAV length = %d, want %d", len(wav), 44+len(pcm))
	}
}

func TestWAV_Stereo(t *testing.T) {
	pcm := make([]byte, 100)
	wav := WAV(pcm, 44100, 2)
	if len(wav) != 44+100 {
		t.Errorf("stereo WAV length = %d, want %d", len(wav), 144)
	}
}

func TestVoiceInput_DisabledByDefault(t *testing.T) {
	vi := NewVoiceInput(DefaultCaptureConfig(), &StubTranscriber{Text: "hello"})
	if vi.Enabled() {
		t.Error("VoiceInput should be disabled by default")
	}
}

func TestVoiceInput_EnableDisable(t *testing.T) {
	vi := NewVoiceInput(DefaultCaptureConfig(), &StubTranscriber{})
	vi.Enable()
	if !vi.Enabled() {
		t.Error("should be enabled after Enable()")
	}
	vi.Disable()
	if vi.Enabled() {
		t.Error("should be disabled after Disable()")
	}
}

func TestVoiceInput_RecordWhileDisabled(t *testing.T) {
	vi := NewVoiceInput(DefaultCaptureConfig(), &StubTranscriber{})
	_, err := vi.RecordAndTranscribe(context.Background())
	if err == nil {
		t.Error("expected error when recording while disabled")
	}
}

func TestStubTranscriber(t *testing.T) {
	stub := &StubTranscriber{Text: "transcribed text"}
	result, err := stub.Transcribe(context.Background(), []byte{0, 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "transcribed text" {
		t.Errorf("Transcribe() = %q, want %q", result, "transcribed text")
	}
}

func TestCapture_NotActiveByDefault(t *testing.T) {
	ac := NewCapture(DefaultCaptureConfig())
	if ac.Active() {
		t.Error("new capture should not be active")
	}
}

func TestCapture_FinishWithoutBegin(t *testing.T) {
	ac := NewCapture(DefaultCaptureConfig())
	_, err := ac.Finish()
	if err == nil {
		t.Error("expected error when finishing without begin")
	}
}

func TestParseIntent_Navigate(t *testing.T) {
	intent := ParseIntent("go to main function")
	if intent.Verb != "navigate" {
		t.Errorf("Verb = %q, want %q", intent.Verb, "navigate")
	}
	if intent.Object != "main function" {
		t.Errorf("Object = %q, want %q", intent.Object, "main function")
	}
}

func TestParseIntent_Search(t *testing.T) {
	intent := ParseIntent("find all todos")
	if intent.Verb != "search" {
		t.Errorf("Verb = %q, want %q", intent.Verb, "search")
	}
}

func TestParseIntent_DefaultInput(t *testing.T) {
	intent := ParseIntent("hello world")
	if intent.Verb != "input" {
		t.Errorf("Verb = %q, want %q", intent.Verb, "input")
	}
	if intent.Object != "hello world" {
		t.Errorf("Object = %q, want %q", intent.Object, "hello world")
	}
}
