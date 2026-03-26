package output

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type Coordinator struct {
	ClipboardPlan   string
	PastePlan       string
	ClipboardBinary string
	PasteBinary     string
	EnableAutoPaste bool
}

type Delivery struct {
	ClipboardWritten bool
	ClipboardMethod  string
	PasteExecuted    bool
	PasteMethod      string
}

func (c Coordinator) Summary() string {
	return fmt.Sprintf("clipboard=%s, paste=%s", c.ClipboardPlan, c.PastePlan)
}

func (c Coordinator) Deliver(ctx context.Context, text string) (Delivery, error) {
	result := Delivery{}
	if text == "" {
		return result, nil
	}

	if err := c.writeClipboard(ctx, text, &result); err != nil {
		return result, err
	}

	if err := c.autoPaste(ctx, &result); err != nil {
		return result, err
	}

	return result, nil
}

func (c Coordinator) writeClipboard(ctx context.Context, text string, result *Delivery) error {
	if c.ClipboardBinary == "" {
		return fmt.Errorf("clipboard output is not configured")
	}

	cmd := exec.CommandContext(ctx, c.ClipboardBinary)
	cmd.Stdin = strings.NewReader(text)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clipboard command failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	result.ClipboardWritten = true
	result.ClipboardMethod = filepath.Base(c.ClipboardBinary)
	return nil
}

func (c Coordinator) autoPaste(ctx context.Context, result *Delivery) error {
	if !c.EnableAutoPaste || c.PasteBinary == "" {
		return nil
	}

	switch filepath.Base(c.PasteBinary) {
	case "ydotool":
		cmd := exec.CommandContext(ctx, c.PasteBinary, "key", "29:1", "47:1", "47:0", "29:0")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ydotool paste failed: %w (%s)", err, strings.TrimSpace(string(output)))
		}
		result.PasteExecuted = true
		result.PasteMethod = "ydotool"
		return nil
	default:
		return nil
	}
}
