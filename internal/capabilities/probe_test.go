package capabilities

import (
	"testing"

	"coe/internal/platform/portal"
)

func TestSelectProfileFull(t *testing.T) {
	t.Parallel()

	caps := Capabilities{
		SessionType: "wayland",
		Desktop:     "gnome",
		Portals: portal.Interfaces{
			GlobalShortcuts: portal.InterfaceStatus{Available: true, Version: 2},
			RemoteDesktop:   portal.InterfaceStatus{Available: true, Version: 2},
			Clipboard:       portal.InterfaceStatus{Available: true, Version: 1},
		},
		Binaries: map[string]Binary{
			"pw-record": {Found: true, Path: "/usr/bin/pw-record"},
		},
	}

	caps.Hotkey = planHotkey(caps)
	caps.Audio = planAudio(caps)
	caps.Clipboard = planClipboard(caps)
	caps.Paste = planPaste(caps)

	if got := selectProfile(caps); got != ProfileGNOMEFull {
		t.Fatalf("selectProfile() = %q, want %q", got, ProfileGNOMEFull)
	}
}
