package output

import (
	"fmt"
	"strings"

	"coe/internal/focus"
)

const defaultPasteShortcut = "ctrl+v"

func NormalizePasteShortcut(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return defaultPasteShortcut
	}
	return normalized
}

func ValidatePasteShortcut(value string) error {
	switch NormalizePasteShortcut(value) {
	case "ctrl+v", "ctrl+shift+v", "shift+insert":
		return nil
	default:
		return fmt.Errorf("unsupported paste shortcut %q", value)
	}
}

func ydotoolPasteArgs(shortcut string) ([]string, error) {
	switch NormalizePasteShortcut(shortcut) {
	case "ctrl+v":
		return []string{"key", "29:1", "47:1", "47:0", "29:0"}, nil
	case "ctrl+shift+v":
		return []string{"key", "29:1", "42:1", "47:1", "47:0", "42:0", "29:0"}, nil
	case "shift+insert":
		return []string{"key", "42:1", "110:1", "110:0", "42:0"}, nil
	default:
		return nil, fmt.Errorf("unsupported paste shortcut %q", shortcut)
	}
}

func looksLikeTerminalTarget(target focus.Target) bool {
	joined := strings.ToLower(strings.Join([]string{
		target.AppID,
		target.WMClass,
		target.Title,
	}, " "))

	for _, needle := range []string{
		"ptyxis",
		"kgx",
		"gnome-console",
		"org.gnome.console",
		"gnome-terminal",
		"org.gnome.terminal",
		"wezterm",
		"alacritty",
		"kitty",
		"foot",
		"ghostty",
		"terminal",
		"codex",
	} {
		if strings.Contains(joined, needle) {
			return true
		}
	}

	return false
}
