# GNOME Focus Helper

Coe can choose a different paste shortcut when the focused target is a terminal-like app. On GNOME, the clean way to do that is a small GNOME Shell extension that exposes the focused window over D-Bus.

This is GNOME-only. It is not a cross-desktop interface.

## Why It Exists

`Ctrl+V` works in many GUI text fields. It does not work in many terminals. Terminals usually want `Ctrl+Shift+V` or `Shift+Insert`.

Wayland and the portal APIs let Coe inject a paste shortcut. They do not tell Coe what the current target app is. A GNOME Shell extension can answer that question because Shell already knows the focused window.

## D-Bus Contract

Service:

- `org.quaily.Coe.Focus1`

Path:

- `/org/quaily/Coe/Focus1`

Interface:

- `org.quaily.Coe.Focus1`

Method:

- `GetFocusedWindow() -> (app_id, wm_class, title)`

Return values:

- `app_id`
  GTK app id or sandboxed app id when available.
- `wm_class`
  WM class as a fallback for apps that do not expose an app id.
- `title`
  Current focused window title.

The Go side treats empty strings as "unknown" and falls back to the default paste shortcut.

## Shell Side

The extension should read the current focus from Mutter / GNOME Shell:

- `global.display.get_focus_window()`
- `window.get_gtk_application_id()`
- `window.get_sandboxed_app_id()`
- `window.get_wm_class()`
- `window.get_title()`

The extension only needs to answer the focused-window query. It does not need to inject input itself.

## Coe Side

When `output.use_gnome_focus_helper: true`:

1. Coe connects to the session bus.
2. Coe calls `GetFocusedWindow()` before auto-paste.
3. Coe classifies the target.
4. If the target looks terminal-like, Coe uses `output.terminal_paste_shortcut`.
5. Otherwise Coe uses `output.paste_shortcut`.

Current built-in terminal-like matchers include:

- `ptyxis`
- `kgx`
- `gnome-console`
- `gnome-terminal`
- `wezterm`
- `alacritty`
- `kitty`
- `foot`
- `ghostty`
- `terminal`
- `codex`

This matcher list is intentionally simple. It can be expanded later.

## Failure Mode

If the helper is disabled, missing, or errors at runtime:

- Coe falls back to `output.paste_shortcut`
- Coe does not block dictation
- startup logs will show a warning if the helper was enabled but unavailable

## Example Config

```yaml
output:
  enable_auto_paste: true
  paste_shortcut: ctrl+v
  terminal_paste_shortcut: ctrl+shift+v
  use_gnome_focus_helper: true
```

New default configs already enable `use_gnome_focus_helper: true`.
