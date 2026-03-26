package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"coe/internal/asr"
	"coe/internal/audio"
	"coe/internal/llm"
	"coe/internal/output"
)

type Orchestrator struct {
	Recorder  audio.Recorder
	ASR       asr.Client
	Corrector llm.Corrector
	Output    *output.Coordinator
}

type Result struct {
	ByteCount          int
	AudioActivity      audio.Activity
	Transcript         string
	TranscriptWarning  string
	Corrected          string
	CorrectionWarning  string
	Output             output.Delivery
	ASRDuration        time.Duration
	CorrectionDuration time.Duration
	OutputDuration     time.Duration
	TotalDuration      time.Duration
}

func (o Orchestrator) Summary() string {
	outputSummary := "disabled"
	if o.Output != nil {
		outputSummary = o.Output.Summary()
	}
	return fmt.Sprintf(
		"recorder=%s, asr=%s, llm=%s, output={%s}",
		o.Recorder.Summary(),
		o.ASR.Name(),
		o.Corrector.Name(),
		outputSummary,
	)
}

func (o Orchestrator) ProcessCapture(ctx context.Context, capture audio.Result) (Result, error) {
	startedAt := time.Now()
	result := Result{
		ByteCount: capture.ByteCount,
	}
	if capture.ByteCount == 0 {
		result.TotalDuration = time.Since(startedAt)
		return result, nil
	}

	result.AudioActivity = audio.AnalyzeActivity(capture, audio.DefaultActivityThresholds())
	if result.AudioActivity.Supported && result.AudioActivity.ApproxSilent {
		result.TranscriptWarning = "captured audio is near-silent; skipped transcription"
		result.TotalDuration = time.Since(startedAt)
		return result, nil
	}
	if result.AudioActivity.Supported && result.AudioActivity.ApproxCorrupt {
		result.TranscriptWarning = "captured audio appears saturated or corrupted; skipped transcription"
		result.TotalDuration = time.Since(startedAt)
		return result, nil
	}

	asrStartedAt := time.Now()
	transcribed, err := o.ASR.Transcribe(ctx, capture)
	result.ASRDuration = time.Since(asrStartedAt)
	if err != nil {
		return Result{}, err
	}
	result.Transcript = transcribed.Text
	if strings.TrimSpace(transcribed.Warning) != "" {
		result.TranscriptWarning = transcribed.Warning
	}
	if strings.TrimSpace(result.Transcript) == "" {
		if result.TranscriptWarning == "" {
			result.TranscriptWarning = "ASR returned empty transcript; skipped correction and output"
		}
		result.TotalDuration = time.Since(startedAt)
		return result, nil
	}

	correctionStartedAt := time.Now()
	corrected, err := o.Corrector.Correct(ctx, transcribed.Text)
	result.CorrectionDuration = time.Since(correctionStartedAt)
	if err != nil {
		result.CorrectionWarning = err.Error()
		result.Corrected = transcribed.Text
	} else {
		result.Corrected = corrected.Text
	}
	if strings.TrimSpace(result.Corrected) == "" {
		result.Corrected = transcribed.Text
		if result.CorrectionWarning == "" {
			result.CorrectionWarning = "correction returned empty text; fell back to transcript"
		}
	}

	if o.Output != nil {
		outputStartedAt := time.Now()
		delivery, err := o.Output.Deliver(ctx, result.Corrected)
		result.OutputDuration = time.Since(outputStartedAt)
		if err != nil {
			return Result{}, err
		}
		result.Output = delivery
	}

	result.TotalDuration = time.Since(startedAt)
	return result, nil
}
