package app

import "testing"

func TestDictationStateTransitionsPreserveSession(t *testing.T) {
	state := newDictationState()

	initial := state.Snapshot()
	if initial.State != "idle" || initial.SessionID != "" {
		t.Fatalf("initial snapshot = %#v, want idle without session", initial)
	}

	recording := state.Recording("recording started")
	if recording.State != "recording" || recording.SessionID == "" {
		t.Fatalf("recording snapshot = %#v, want recording with session id", recording)
	}

	processing := state.Processing("processing")
	if processing.State != "processing" || processing.SessionID != recording.SessionID {
		t.Fatalf("processing snapshot = %#v, want same session id %q", processing, recording.SessionID)
	}

	completed := state.Completed("done")
	if completed.State != "completed" || completed.SessionID != recording.SessionID {
		t.Fatalf("completed snapshot = %#v, want same session id %q", completed, recording.SessionID)
	}
}

func TestDictationStateAllocatesNewSessionPerRecording(t *testing.T) {
	state := newDictationState()

	first := state.Recording("first")
	state.Completed("done")
	second := state.Recording("second")

	if first.SessionID == second.SessionID {
		t.Fatalf("session ids are equal: %q", first.SessionID)
	}
}
