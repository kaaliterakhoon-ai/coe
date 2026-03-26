package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"coe/internal/notify"
	"coe/internal/pipeline"
)

const notificationTimeout = 3 * time.Second

func (a *App) emitNotification(w io.Writer, msg notify.Message) {
	if a.Notifier == nil || msg.Title == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
	defer cancel()

	if err := a.Notifier.Send(ctx, msg); err != nil {
		fmt.Fprintf(w, "notification warning: %v\n", err)
	}
}

func (a *App) notificationForStart() notify.Message {
	if !a.Config.Notifications.NotifyOnRecordingStart {
		return notify.Message{}
	}

	return notify.Message{
		Title:   "Dictation started",
		Body:    "Speak now, then trigger again to stop recording.",
		Urgency: notify.UrgencyLow,
		Timeout: 2200 * time.Millisecond,
	}
}

func (a *App) notificationForProcessing(result pipeline.Result) notify.Message {
	if result.Transcript == "" {
		return notify.Message{
			Title:   "No speech detected",
			Body:    normalizeBody(result.TranscriptWarning, "No transcript was produced. The microphone input may be muted, too quiet, near-silent, or corrupted."),
			Urgency: notify.UrgencyNormal,
			Timeout: 4500 * time.Millisecond,
		}
	}

	status := "Text copied to the clipboard."
	if result.Output.PasteExecuted {
		status = "Text copied and pasted into the focused app."
	} else if result.Output.ClipboardWritten {
		status = "Text copied to the clipboard."
	} else {
		status = "Text was transcribed, but no delivery action completed."
	}
	if result.Output.PasteWarning != "" {
		status = status + " Auto-paste needs attention."
	}

	lines := []string{}
	if a.Config.Notifications.ShowTextPreview {
		lines = append(lines, previewText(result.Corrected, 140))
	}
	lines = append(lines, status)
	if result.CorrectionWarning != "" {
		lines = append(lines, "Correction fell back to the raw transcript.")
	}

	return notify.Message{
		Title:   "Dictation complete",
		Body:    strings.Join(compact(lines), "\n"),
		Urgency: notify.UrgencyNormal,
		Timeout: 5000 * time.Millisecond,
	}
}

func notificationForFailure(title string, err error) notify.Message {
	if err == nil {
		return notify.Message{}
	}

	return notify.Message{
		Title:   title,
		Body:    err.Error(),
		Urgency: notify.UrgencyCritical,
		Timeout: 6000 * time.Millisecond,
	}
}

func normalizeBody(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func previewText(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if limit <= 0 || len([]rune(text)) <= limit {
		return text
	}

	runes := []rune(text)
	return string(runes[:limit]) + "..."
}

func compact(lines []string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
