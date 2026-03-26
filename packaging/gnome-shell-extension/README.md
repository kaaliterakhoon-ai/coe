# GNOME Shell Extension

This directory contains a minimal GNOME Shell extension for focus-aware paste.

Install path:

- `~/.local/share/gnome-shell/extensions/coe-focus-helper@quaily.com`

Quick install:

```bash
mkdir -p ~/.local/share/gnome-shell/extensions
cp -r packaging/gnome-shell-extension/coe-focus-helper@quaily.com \
  ~/.local/share/gnome-shell/extensions/
```

Then enable it with GNOME Extensions or `gnome-extensions enable coe-focus-helper@quaily.com`.

After that, set:

```yaml
output:
  use_gnome_focus_helper: true
```

The extension exports:

- service: `org.quaily.Coe.Focus1`
- path: `/org/quaily/Coe/Focus1`
- interface: `org.quaily.Coe.Focus1`
