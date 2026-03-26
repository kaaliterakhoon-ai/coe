package pipeline

import (
	"context"
	"testing"

	"coe/internal/asr"
	"coe/internal/audio"
	"coe/internal/llm"
	"coe/internal/output"
)

func TestProcessCaptureSkipsCorrectionWhenTranscriptIsEmpty(t *testing.T) {
	corrector := &fakeCorrector{}
	coordinator := &output.Coordinator{}

	orchestrator := Orchestrator{
		Recorder:  fakeRecorder{},
		ASR:       fakeASR{text: ""},
		Corrector: corrector,
		Output:    coordinator,
	}

	result, err := orchestrator.ProcessCapture(context.Background(), audio.Result{
		Data:       []byte{1, 2, 3, 4},
		ByteCount:  4,
		SampleRate: 16000,
		Channels:   1,
		Format:     "s16",
	})
	if err != nil {
		t.Fatalf("ProcessCapture() error = %v", err)
	}
	if result.TranscriptWarning == "" {
		t.Fatal("expected transcript warning")
	}
	if corrector.calls != 0 {
		t.Fatalf("corrector calls = %d, want 0", corrector.calls)
	}
	if result.Output.ClipboardWritten {
		t.Fatal("expected output stage to be skipped")
	}
}

type fakeRecorder struct{}

func (fakeRecorder) Start(context.Context) (audio.CaptureSession, error) {
	panic("unexpected call")
}

func (fakeRecorder) Summary() string {
	return "fake-recorder"
}

type fakeASR struct {
	text string
}

func (f fakeASR) Transcribe(context.Context, audio.Result) (asr.Result, error) {
	return asr.Result{Text: f.text}, nil
}

func (f fakeASR) Name() string {
	return "fake-asr"
}

type fakeCorrector struct {
	calls int
}

func (f *fakeCorrector) Correct(context.Context, string) (llm.Result, error) {
	f.calls++
	return llm.Result{Text: "unused"}, nil
}

func (f *fakeCorrector) Name() string {
	return "fake-llm"
}
