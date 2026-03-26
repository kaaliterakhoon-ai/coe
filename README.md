# Coe (聲)

Coe is a dictation tool for GNOME on Wayland, written in Go.

It is a Linux-focused recreation of [`missuo/koe`](https://github.com/missuo/koe). The goal is the same: press a hotkey, speak, clean up the transcript, and put the result back into the active app.

## The Name

`coe` is close to `koe` on purpose. The project is a nod to Koe, but aimed at Linux and Wayland. The old kanji character `聲` means voice. That is the whole point of the tool.

## Why Coe?

Most Linux voice input tools fail in one of three ways. They depend on X11-era assumptions. They hide basic configuration behind a GUI. Or they do not fit the Wayland security model at all.

Coe takes a narrower path:

- It is GNOME-first and Wayland-first.
- It runs in the background and keeps the UI surface small.
- It stores configuration in plain YAML.
- It uses the platform path where possible: portal clipboard, portal paste, desktop notifications.
- It keeps the degraded path explicit when Wayland blocks the ideal one.

The scope is deliberately narrow. Coe is trying to do one job well.

## How It Works

The verified path today is:

`GNOME custom shortcut -> coe trigger toggle -> pw-record -> OpenAI ASR -> OpenAI LLM correction -> portal clipboard -> portal auto-paste`

The runtime flow is:

1. Trigger dictation with `coe trigger toggle`, usually from a GNOME custom shortcut that Coe ensures at startup when `GlobalShortcuts` is unavailable.
2. Record microphone input with `pw-record`.
3. Reject near-silent or obviously corrupt captures before they leave the machine.
4. Send the audio to OpenAI Audio Transcriptions.
5. Send the transcript to an OpenAI-compatible text model for cleanup.
6. Write the corrected text through the clipboard path.
7. Paste it back into the focused app when the runtime allows it.

Current provider support is intentionally narrow:

- ASR: OpenAI Audio Transcriptions
- ASR: optional local `whisper.cpp` provider through `whisper-cli`
- LLM correction: OpenAI-compatible Responses API or Chat Completions through `uniai`
- Output: portal clipboard and portal paste first, `wl-copy` and `ydotool` as fallbacks

## Installation

### Requirements

Runtime requirements:

- Wayland session
- GNOME desktop
- `pw-record`
- `wl-copy`
- `OPENAI_API_KEY`

You can keep the key in `~/.config/coe/env`, or write it directly into `asr.api_key` and `llm.api_key` in `config.yaml`.

Optional:

- `ydotool` if you want to try the command-line paste fallback
- `whisper-cli` and a Whisper model file if you want local ASR

On Ubuntu, you can install the command-line dependencies with:

```bash
sudo apt update
sudo apt install -y pipewire-bin wl-clipboard
```

Optional paste fallback:

```bash
sudo apt install -y ydotool
```

### Release

GitHub Actions builds Linux release artifacts with GoReleaser.

- Pull requests and pushes to the default branch run a snapshot build and upload Linux artifacts to the workflow run.
- Tags that match `v*` run `goreleaser release` and publish Linux binaries, tarballs, and checksums to the GitHub release.

The release config lives in [`.goreleaser.yaml`](./.goreleaser.yaml). The workflow lives in [`.github/workflows/release.yml`](./.github/workflows/release.yml).

### Build from Source

#### Prerequisites

- Go `1.24+`
- a Linux machine
- the runtime requirements listed above if you want to run the built binary

#### Build

```bash
git clone https://github.com/quailyquaily/coe.git
cd coe
go build -o coe ./cmd/coe
```

#### Run

```bash
./coe serve
```

### Install As A User Service

To install the current alpha as a persistent user service:

```bash
./scripts/install-user.sh
```

The script installs:

- `~/.local/bin/coe`
- `~/.config/systemd/user/coe.service`
- `~/.config/coe/env`
- `~/.local/share/gnome-shell/extensions/coe-focus-helper@quaily.com`

Then put your OpenAI key into `~/.config/coe/env` and restart the service:

```bash
systemctl --user restart coe.service
```

If you prefer, you can leave `~/.config/coe/env` empty and store the key in `asr.api_key` and `llm.api_key` instead.

## Configuration

Coe keeps its config in plain files.

Config file:

- `~/.config/coe/config.yaml`
- repo example: [`config.example.yaml`](./config.example.yaml)

Runtime state:

- `XDG_STATE_HOME/coe/state.json`
- fallback: `~/.config/coe/state.json`

The state file stores the portal restore token used to avoid repeated authorization prompts when the desktop backend accepts persistence.

Create the default config with:

```bash
go run ./cmd/coe config init
```

That writes `~/.config/coe/config.yaml`, unless `COE_CONFIG` overrides the path.

Or start from the repo example:

```bash
cp config.example.yaml ~/.config/coe/config.yaml
```

The current defaults are:

### ASR

- provider: `openai`
- endpoint: `https://api.openai.com/v1/audio/transcriptions`
- model: `gpt-4o-mini-transcribe`
- direct key field: `asr.api_key`
- api key env: `OPENAI_API_KEY`

To switch ASR to local `whisper.cpp`, set:

```yaml
asr:
  provider: whispercpp
  endpoint: ""
  model: ""
  language: zh
  api_key: ""
  api_key_env: ""
  binary: whisper-cli
  model_path: /absolute/path/to/ggml-base.bin
  threads: 4
  use_gpu: false
```

Notes:

- `binary` defaults to `whisper-cli`
- `model_path` is required for `whisper.cpp`
- `prompt` is passed as the initial prompt
- `threads` defaults to `GOMAXPROCS`
- `use_gpu: false` adds `--no-gpu`

### LLM correction

- provider: `openai`
- endpoint type: `chat`
- endpoint: `https://api.openai.com/v1`
- model: `gpt-4o-mini`
- direct key field: `llm.api_key`
- api key env: `OPENAI_API_KEY`

Set `llm.endpoint_type: responses` if you want Coe to call the OpenAI Responses API instead. The default route is Chat Completions through `uniai`.

### Audio

- recorder: `pw-record`
- sample rate: `16000`
- channels: `1`
- format: `s16`

### Output

- clipboard: `wl-copy`
- clipboard and paste prefer portal paths when the runtime exposes them
- `wl-copy` and `ydotool` remain command-line fallbacks
- GNOME focus-aware paste is enabled by default in new configs and can switch from `Ctrl+V` to `Ctrl+Shift+V` for terminal-like targets

### Notifications

- `enable_system: true`
- `show_text_preview: true`
- `notify_on_recording_start: false`

### Runtime

- `log_level: info`
- set `log_level: debug` to print per-stage timings and output fallback details
- or override it for one run: `coe serve --log-level debug`

See [`config.example.yaml`](./config.example.yaml) and [`docs/gnome-focus-helper.md`](./docs/gnome-focus-helper.md) for the GNOME focus-aware paste route. Existing configs may need `output.use_gnome_focus_helper: true` added manually.

## Current Behavior

What works:

- GNOME Wayland fallback trigger via an auto-managed GNOME custom shortcut that runs `coe trigger toggle`
- microphone capture through `pw-record`
- batch transcription through OpenAI Audio Transcriptions
- transcript cleanup through OpenAI Responses
- final text written through portal clipboard
- final text auto-pasted through portal keyboard injection
- GNOME desktop notifications for completion and failure
- near-silent recordings are short-circuited locally before ASR
- severely clipped or corrupted recordings are short-circuited locally before ASR

What does not exist yet:

- `GlobalShortcuts` portal support
- a KDE or Hyprland validation pass in this repo
- a stronger answer for the upstream microphone/PipeWire saturation issue

Portal access persistence:

- If `persist_portal_access` is `true`, Coe stores the portal restore token locally.
- After the first successful authorization, later runs should reuse that token instead of prompting every time.
- If GNOME or the portal backend rejects the stored token, Coe falls back to a fresh authorization flow.

System notifications:

- By default, Coe sends GNOME desktop notifications for completed dictation and failure cases.
- Near-silent or corrupt captures are reported locally and skipped before network transcription.
- Recording-start notifications stay off by default.

## Commands

- `go run ./cmd/coe doctor`
- `go run ./cmd/coe config init`
- `go run ./cmd/coe serve`
- `go run ./cmd/coe trigger toggle`
- `go run ./cmd/coe trigger start`
- `go run ./cmd/coe trigger stop`
- `go run ./cmd/coe trigger status`
- `go run ./cmd/coe version`

## Docs

- [`docs/README.md`](./docs/README.md)
- [`docs/install.md`](./docs/install.md)
- [`docs/architecture.md`](./docs/architecture.md)
- [`docs/fallbacks.md`](./docs/fallbacks.md)
- [`docs/gnome-globalshortcuts-matrix.md`](./docs/gnome-globalshortcuts-matrix.md)
- [`docs/pw-record-exit-status.md`](./docs/pw-record-exit-status.md)
- [`docs/roadmap.md`](./docs/roadmap.md)
