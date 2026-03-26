package state

import (
	"path/filepath"
	"testing"
)

func TestStoreLoadSave(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.RemoteDesktopRestoreToken != "" {
		t.Fatalf("unexpected initial token %q", loaded.RemoteDesktopRestoreToken)
	}

	want := PortalAccess{RemoteDesktopRestoreToken: "restore-token-123"}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after save error = %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}
