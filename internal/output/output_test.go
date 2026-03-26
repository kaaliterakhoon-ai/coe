package output

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliverWritesClipboard(t *testing.T) {
	dir := t.TempDir()
	clipboardSink := filepath.Join(dir, "clipboard.txt")
	clipboardBin := filepath.Join(dir, "fake-wl-copy.sh")

	script := "#!/bin/sh\ncat > \"$COE_CLIPBOARD_SINK\"\n"
	if err := os.WriteFile(clipboardBin, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("COE_CLIPBOARD_SINK", clipboardSink)

	coord := Coordinator{
		ClipboardPlan:   "command",
		PastePlan:       "unavailable",
		ClipboardBinary: clipboardBin,
	}

	delivery, err := coord.Deliver(context.Background(), "hello clipboard")
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if !delivery.ClipboardWritten {
		t.Fatal("expected clipboard to be written")
	}

	data, err := os.ReadFile(clipboardSink)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "hello clipboard" {
		t.Fatalf("clipboard contents = %q, want %q", got, "hello clipboard")
	}
}

func TestDeliverRunsYdotoolPaste(t *testing.T) {
	dir := t.TempDir()
	clipboardSink := filepath.Join(dir, "clipboard.txt")
	pasteSink := filepath.Join(dir, "paste.txt")
	clipboardBin := filepath.Join(dir, "fake-wl-copy.sh")
	pasteBin := filepath.Join(dir, "ydotool")

	if err := os.WriteFile(clipboardBin, []byte("#!/bin/sh\ncat > \"$COE_CLIPBOARD_SINK\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(clipboard) error = %v", err)
	}
	if err := os.WriteFile(pasteBin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$COE_PASTE_SINK\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(paste) error = %v", err)
	}

	t.Setenv("COE_CLIPBOARD_SINK", clipboardSink)
	t.Setenv("COE_PASTE_SINK", pasteSink)

	coord := Coordinator{
		ClipboardPlan:   "command",
		PastePlan:       "command",
		ClipboardBinary: clipboardBin,
		PasteBinary:     pasteBin,
		EnableAutoPaste: true,
	}

	delivery, err := coord.Deliver(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if !delivery.PasteExecuted {
		t.Fatal("expected paste to be executed")
	}

	data, err := os.ReadFile(pasteSink)
	if err != nil {
		t.Fatalf("ReadFile(paste) error = %v", err)
	}
	got := strings.TrimSpace(string(data))
	want := "key\n29:1\n47:1\n47:0\n29:0"
	if got != want {
		t.Fatalf("paste command args = %q, want %q", got, want)
	}
}
