package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const envConfigPath = "COE_CONFIG"

type Config struct {
	Runtime RuntimeConfig `json:"runtime"`
	Hotkey  HotkeyConfig  `json:"hotkey"`
	Audio   AudioConfig   `json:"audio"`
	ASR     Provider      `json:"asr"`
	LLM     Provider      `json:"llm"`
	Output  OutputConfig  `json:"output"`
}

type RuntimeConfig struct {
	TargetDesktop        string `json:"target_desktop"`
	AllowExternalTrigger bool   `json:"allow_external_trigger"`
}

type HotkeyConfig struct {
	Name                 string `json:"name"`
	Description          string `json:"description"`
	PreferredAccelerator string `json:"preferred_accelerator"`
}

type AudioConfig struct {
	RecorderBinary string `json:"recorder_binary"`
	SampleRate     int    `json:"sample_rate"`
	Channels       int    `json:"channels"`
	Format         string `json:"format"`
}

type Provider struct {
	Kind      string `json:"kind"`
	Endpoint  string `json:"endpoint"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"api_key_env"`
	Language  string `json:"language"`
	Prompt    string `json:"prompt"`
}

type OutputConfig struct {
	PreferredClipboardMode string `json:"preferred_clipboard_mode"`
	EnableAutoPaste        bool   `json:"enable_auto_paste"`
	PersistPortalAccess    bool   `json:"persist_portal_access"`
	ClipboardBinary        string `json:"clipboard_binary"`
	PasteBinary            string `json:"paste_binary"`
}

func Default() Config {
	return Config{
		Runtime: RuntimeConfig{
			TargetDesktop:        "gnome",
			AllowExternalTrigger: true,
		},
		Hotkey: HotkeyConfig{
			Name:                 "push-to-talk",
			Description:          "Press and hold to start dictation.",
			PreferredAccelerator: "<Ctrl><Alt>space",
		},
		Audio: AudioConfig{
			RecorderBinary: "pw-record",
			SampleRate:     16000,
			Channels:       1,
			Format:         "s16",
		},
		ASR: Provider{
			Kind:      "openai",
			Endpoint:  "https://api.openai.com/v1/audio/transcriptions",
			Model:     "gpt-4o-mini-transcribe",
			APIKeyEnv: "OPENAI_API_KEY",
			Language:  "zh",
			Prompt:    "",
		},
		LLM: Provider{
			Kind:      "openai",
			Endpoint:  "https://api.openai.com/v1/responses",
			Model:     "gpt-4o-mini",
			APIKeyEnv: "OPENAI_API_KEY",
			Prompt:    "",
		},
		Output: OutputConfig{
			PreferredClipboardMode: "portal",
			EnableAutoPaste:        true,
			PersistPortalAccess:    true,
			ClipboardBinary:        "wl-copy",
			PasteBinary:            "",
		},
	}
}

func ResolvePath() (string, error) {
	if path := os.Getenv(envConfigPath); path != "" {
		return path, nil
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(base, "coe", "config.json"), nil
}

func LoadOrDefault(path string) (Config, error) {
	cfg, err := Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}

	return cfg, err
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func WriteDefault(path string, overwrite bool) (bool, error) {
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}

	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return false, err
	}

	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return false, err
	}

	return true, nil
}
