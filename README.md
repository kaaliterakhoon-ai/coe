# COE (聲)

COE is a dictation tool for GNOME on Wayland, written in Go.

It is a Linux-focused recreation of [`missuo/koe`](https://github.com/missuo/koe). The goal is the same: capture speech, transcribe it, clean up the text, and put the result back into the active app.

## Current status

The verified path today is:

`GNOME custom shortcut -> coe trigger toggle -> pw-record -> OpenAI ASR -> OpenAI LLM correction -> portal clipboard -> portal auto-paste`

What works:

- GNOME Wayland fallback trigger via `coe trigger toggle`
- microphone capture through `pw-record`
- batch transcription through OpenAI Audio Transcriptions
- transcript cleanup through OpenAI Responses
- final text written through portal clipboard
- final text auto-pasted through portal keyboard injection
- near-silent recordings are short-circuited locally before ASR
- severely clipped or corrupted recordings are short-circuited locally before ASR
- command-line fallbacks through `wl-copy` and `ydotool`

What does not exist yet:

- `GlobalShortcuts` portal is not implemented yet
- `ydotool` remains the command-line paste fallback

Portal access persistence:

- If `persist_portal_access` is `true`, COE stores the portal restore token locally.
- After the first successful authorization, later runs should reuse that token instead of prompting every time.
- If GNOME or the portal backend rejects the stored token, COE falls back to a fresh authorization flow.

System notifications:

- By default, COE sends GNOME desktop notifications for completed dictation and failure cases.
- Near-silent or corrupt captures are reported locally and skipped before network transcription.
- Recording-start notifications stay off by default.

## Requirements

- Wayland session
- GNOME desktop
- `pw-record`
- `wl-copy`
- `OPENAI_API_KEY`

Optional:

- `ydotool` if you want to experiment with auto-paste fallback later

## Quick start

Create a config file:

```bash
go run ./cmd/coe config init
```

This writes the default config to `~/.config/coe/config.yaml`, unless `COE_CONFIG` overrides the path.

Export your OpenAI API key:

```bash
export OPENAI_API_KEY=...
```

Check runtime capabilities:

```bash
go run ./cmd/coe doctor
```

Start the daemon:

```bash
go run ./cmd/coe serve
```

Trigger dictation by hand:

```bash
go run ./cmd/coe trigger toggle
```

If GNOME Wayland does not expose `GlobalShortcuts`, add a GNOME custom shortcut that runs:

```bash
coe trigger toggle
```

## Install As A User Service

To install the current alpha as a persistent user service:

```bash
./scripts/install-user.sh
```

The script installs:

- `~/.local/bin/coe`
- `~/.config/systemd/user/coe.service`
- `~/.config/coe/env`

Then put your OpenAI key into `~/.config/coe/env` and restart the service:

```bash
systemctl --user restart coe.service
```

## Defaults

ASR:

- endpoint: `https://api.openai.com/v1/audio/transcriptions`
- model: `gpt-4o-mini-transcribe`
- api key env: `OPENAI_API_KEY`

LLM correction:

- endpoint: `https://api.openai.com/v1/responses`
- model: `gpt-4o-mini`
- api key env: `OPENAI_API_KEY`

Audio:

- recorder: `pw-record`
- sample rate: `16000`
- channels: `1`
- format: `s16`

Output:

- clipboard: `wl-copy`
- clipboard and paste will prefer portal paths when the runtime exposes them
- `wl-copy` and `ydotool` remain command-line fallbacks

Notifications:

- `enable_system: true`
- `show_text_preview: true`
- `notify_on_recording_start: false`

## Commands

- `go run ./cmd/coe doctor`
- `go run ./cmd/coe config init`
- `go run ./cmd/coe serve`
- `go run ./cmd/coe trigger toggle`
- `go run ./cmd/coe trigger start`
- `go run ./cmd/coe trigger stop`
- `go run ./cmd/coe trigger status`

## Docs

- [`docs/README.md`](./docs/README.md)
- [`docs/install.md`](./docs/install.md)
- [`docs/architecture.md`](./docs/architecture.md)
- [`docs/fallbacks.md`](./docs/fallbacks.md)
- [`docs/gnome-globalshortcuts-matrix.md`](./docs/gnome-globalshortcuts-matrix.md)
- [`docs/pw-record-exit-status.md`](./docs/pw-record-exit-status.md)
- [`docs/roadmap.md`](./docs/roadmap.md)
