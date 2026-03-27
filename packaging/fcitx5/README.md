# Coe Fcitx5 Module

This directory contains the first thin Fcitx5 module skeleton for Coe.

Current scope:

- registers as a Fcitx5 module
- watches key events in `PostInputMethod`
- matches the hard-coded trigger `<Shift><Super>d`
- calls `com.mistermorph.Coe.Dictation1.Toggle()` over session D-Bus
- subscribes to `ResultReady` / `ErrorRaised` over session D-Bus
- dispatches the result back to the Fcitx main event loop
- commits the final text to the current focused input context

It does not do these things yet:

- configurable hotkeys
- clipboard fallback
- polished runtime installation / reload flow

## Build

```bash
./scripts/build-fcitx-module.sh
```

## Install

For distribution-packaged Fcitx5, the reliable path is the system addon directory.

### System install

```bash
sudo INSTALL_SCOPE=system ./scripts/install-fcitx-module.sh --system
```

This should place files under:

- `/usr/lib/x86_64-linux-gnu/fcitx5/libcoefcitx.so`
- `/usr/share/fcitx5/addon/coe.conf`

### User-local install

```bash
./scripts/install-fcitx-module.sh
```

This places files under:

- `~/.local/lib/x86_64-linux-gnu/fcitx5/libcoefcitx.so`
- `~/.local/share/fcitx5/addon/coe.conf`

This path is convenient for iteration, but it may not be picked up by every distribution build of Fcitx5.

Set this in `~/.config/coe/config.yaml` before testing the module:

```yaml
runtime:
  mode: fcitx
```

The install script will try to restart Fcitx5 with:

```bash
fcitx5 -rd
```

If that does not pick up the new module, log out and back in.
