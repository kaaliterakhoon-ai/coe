package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"coe/internal/asr"
	"coe/internal/audio"
	"coe/internal/capabilities"
	"coe/internal/config"
	"coe/internal/control"
	"coe/internal/focus"
	"coe/internal/hotkey"
	"coe/internal/llm"
	"coe/internal/notify"
	"coe/internal/output"
	"coe/internal/pipeline"
	"coe/internal/platform/gnome"
	"coe/internal/state"
)

type App struct {
	Config            config.Config
	Caps              capabilities.Capabilities
	Hotkey            hotkey.Service
	ExternalHotkey    *hotkey.ExternalTriggerService
	ControlSocketPath string
	Notifier          notify.Service
	StartupWarnings   []string
	Pipeline          pipeline.Orchestrator
	resourceClosers   []io.Closer
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	caps, err := capabilities.Probe(ctx)
	if err != nil {
		return nil, err
	}

	recorder := audio.PWRecord{
		Binary:     cfg.Audio.RecorderBinary,
		SampleRate: cfg.Audio.SampleRate,
		Channels:   cfg.Audio.Channels,
		Format:     cfg.Audio.Format,
	}
	asrClient, err := asr.NewClient(cfg.ASR)
	if err != nil {
		return nil, err
	}
	corrector, err := llm.NewCorrector(cfg.LLM)
	if err != nil {
		return nil, err
	}
	resourceClosers := make([]io.Closer, 0, 1)
	if closer, ok := asrClient.(io.Closer); ok {
		resourceClosers = append(resourceClosers, closer)
	}
	if err := output.ValidatePasteShortcut(cfg.Output.PasteShortcut); err != nil {
		return nil, err
	}
	if err := output.ValidatePasteShortcut(cfg.Output.TerminalPasteShortcut); err != nil {
		return nil, err
	}
	clipboardBinary := cfg.Output.ClipboardBinary
	if binary := caps.Binaries["wl-copy"]; binary.Found {
		clipboardBinary = binary.Path
	}
	pasteBinary := cfg.Output.PasteBinary
	if pasteBinary == "" {
		if binary := caps.Binaries["ydotool"]; binary.Found {
			pasteBinary = binary.Path
		}
	}

	var portalStateStore *state.Store
	if cfg.Output.PersistPortalAccess && caps.Portals.RemoteDesktop.Version >= 2 {
		statePath, err := state.ResolvePath()
		if err != nil {
			return nil, err
		}
		portalStateStore = state.NewStore(statePath)
	}

	description := describeFeature(string(caps.Hotkey.Mode), caps.Hotkey.Detail)
	service := hotkey.Service(hotkey.PlannedService{Description: description})
	var external *hotkey.ExternalTriggerService
	var controlSocketPath string
	startupWarnings := make([]string, 0, 2)

	if caps.Hotkey.Mode == capabilities.ModeExternalBinding {
		external = hotkey.NewExternalTriggerService(description)
		service = external

		socketPath, err := control.ResolveSocketPath()
		if err != nil {
			return nil, err
		}
		controlSocketPath = socketPath

		if cfg.Runtime.TargetDesktop == "gnome" {
			manager := gnome.NewShortcutManager()
			if err := manager.EnsureTriggerShortcut(ctx, cfg.Hotkey.Name, cfg.Hotkey.PreferredAccelerator); err != nil {
				startupWarnings = append(startupWarnings, fmt.Sprintf("GNOME custom shortcut setup failed: %v", err))
			}
		}
	}

	notificationService := notify.Service(notify.Disabled{})
	if cfg.Notifications.EnableSystem {
		service, err := notify.ConnectSession("coe")
		if err != nil {
			startupWarnings = append(startupWarnings, fmt.Sprintf("system notifications unavailable: %v", err))
		} else {
			notificationService = service
		}
	}

	focusProvider := focus.Provider(focus.Disabled{})
	if cfg.Output.UseGNOMEFocusHelper && cfg.Runtime.TargetDesktop == "gnome" {
		provider, err := focus.ConnectGNOMESession()
		if err != nil {
			startupWarnings = append(startupWarnings, fmt.Sprintf("GNOME focus helper unavailable: %v", err))
		} else {
			focusProvider = provider
			resourceClosers = append(resourceClosers, provider)
		}
	}

	instance := &App{
		Config:            cfg,
		Caps:              caps,
		Hotkey:            service,
		ExternalHotkey:    external,
		ControlSocketPath: controlSocketPath,
		Notifier:          notificationService,
		StartupWarnings:   startupWarnings,
		resourceClosers:   resourceClosers,
		Pipeline: pipeline.Orchestrator{
			Recorder:  recorder,
			ASR:       asrClient,
			Corrector: corrector,
			Output: &output.Coordinator{
				ClipboardPlan:         describeFeature(string(caps.Clipboard.Mode), caps.Clipboard.Detail),
				PastePlan:             describeFeature(string(caps.Paste.Mode), caps.Paste.Detail),
				ClipboardBinary:       clipboardBinary,
				PasteBinary:           pasteBinary,
				EnableAutoPaste:       cfg.Output.EnableAutoPaste,
				PasteShortcut:         cfg.Output.PasteShortcut,
				TerminalPasteShortcut: cfg.Output.TerminalPasteShortcut,
				UsePortalClipboard:    caps.Clipboard.Mode == capabilities.ModePortal,
				UsePortalPaste:        caps.Paste.Mode == capabilities.ModePortal,
				PersistPortalAccess:   cfg.Output.PersistPortalAccess && caps.Portals.RemoteDesktop.Version >= 2,
				FocusProvider:         focusProvider,
				PortalStateStore:      portalStateStore,
			},
		},
	}

	return instance, nil
}

func (a *App) Serve(ctx context.Context, w io.Writer) error {
	defer func() {
		for _, closer := range a.resourceClosers {
			_ = closer.Close()
		}
		if a.Notifier != nil {
			_ = a.Notifier.Close()
		}
		if a.Pipeline.Output != nil {
			_ = a.Pipeline.Output.Close()
		}
	}()

	logger := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: parseLogLevel(a.Config.Runtime.LogLevel),
	}))

	logger.Info("coe starting")
	logger.Info("runtime capabilities", "report", strings.TrimSpace(a.Caps.Report()))
	wiringAttrs := []any{
		"hotkey", a.Hotkey.Plan(),
		"pipeline", a.Pipeline.Summary(),
		"notifications", blankIfEmpty(a.Notifier.Summary(), "disabled"),
		"paste_shortcut", output.NormalizePasteShortcut(a.Config.Output.PasteShortcut),
		"terminal_paste_shortcut", output.NormalizePasteShortcut(a.Config.Output.TerminalPasteShortcut),
		"gnome_focus_helper", a.Config.Output.UseGNOMEFocusHelper,
	}
	if a.ControlSocketPath != "" {
		wiringAttrs = append(wiringAttrs, "control_socket", a.ControlSocketPath)
	}
	logger.Info("runtime wiring", wiringAttrs...)
	for _, warning := range a.StartupWarnings {
		logger.Warn("startup warning", "warning", warning)
	}
	logger.Info("runtime is scaffolded; waiting for signal")

	var controlErrCh chan error
	if a.ExternalHotkey != nil {
		server, err := control.NewServer(a.ControlSocketPath, a.handleControl)
		if err != nil {
			return err
		}

		controlErrCh = make(chan error, 1)
		go func() {
			controlErrCh <- server.Serve(ctx)
		}()
	}

	events, err := a.Hotkey.Events(ctx)
	if err != nil {
		return err
	}

	var captureSession audio.CaptureSession

	for {
		select {
		case <-ctx.Done():
			if captureSession != nil {
				stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				result, stopErr := captureSession.Stop(stopCtx)
				cancel()
				if stopErr != nil {
					logger.Warn("recording stop during shutdown failed", "error", stopErr)
				} else {
					logger.Info("recording finalized during shutdown", "bytes", result.ByteCount, "duration", result.Duration.Round(time.Millisecond))
				}
			}
			logger.Info("shutting down")
			return nil
		case err := <-controlErrCh:
			if err != nil {
				return err
			}
			controlErrCh = nil
		case event, ok := <-events:
			if !ok {
				logger.Info("hotkey service stopped")
				return nil
			}
			switch event.Type {
			case hotkey.Activated:
				if captureSession != nil {
					logger.Warn("recording already active; ignoring activate event")
					continue
				}

				session, err := a.Pipeline.Recorder.Start(ctx)
				if err != nil {
					logger.Error("recording start failed", "error", err)
					a.emitNotification(logger, notificationForFailure("Recording failed to start", err))
					continue
				}

				captureSession = session
				logger.Info("recording started")
				a.emitNotification(logger, a.notificationForStart())
			case hotkey.Deactivated:
				if captureSession == nil {
					logger.Warn("recording is not active; ignoring deactivate event")
					continue
				}

				stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				result, err := captureSession.Stop(stopCtx)
				cancel()
				captureSession = nil
				if err != nil && result.ByteCount == 0 {
					stopAttrs := []any{"error", err}
					if result.Stderr != "" {
						stopAttrs = append(stopAttrs, "stderr", result.Stderr)
					}
					logger.Error("recording stop failed", stopAttrs...)
					a.emitNotification(logger, notificationForFailure("Recording failed", err))
					continue
				}
				if err != nil {
					logger.Warn("recording stop returned warning", "error", err)
				}

				recordingAttrs := []any{
					"bytes", result.ByteCount,
					"duration", result.Duration.Round(time.Millisecond),
				}
				activity := processedActivityPreview(result)
				if activity != "" {
					recordingAttrs = append(recordingAttrs, "audio_activity", activity)
				}
				if result.Stderr != "" {
					recordingAttrs = append(recordingAttrs, "stderr", result.Stderr)
				}
				logger.Info("recording stopped", recordingAttrs...)
				logger.Debug("capture processing started", "bytes", result.ByteCount)

				processed, err := a.Pipeline.ProcessCapture(ctx, result)
				if err != nil {
					logger.Error("pipeline processing failed", "error", err)
					a.emitNotification(logger, notificationForFailure("Dictation failed", err))
					continue
				}

				pipelineAttrs := []any{
					"transcript", processed.Transcript,
					"corrected", processed.Corrected,
					"asr_duration", processed.ASRDuration.Round(time.Millisecond),
					"correction_duration", processed.CorrectionDuration.Round(time.Millisecond),
					"output_duration", processed.OutputDuration.Round(time.Millisecond),
					"total_duration", processed.TotalDuration.Round(time.Millisecond),
				}
				if processed.AudioActivity.Supported {
					pipelineAttrs = append(pipelineAttrs, "audio_activity", processed.AudioActivity.Summary())
				}
				logger.Info("pipeline result", pipelineAttrs...)
				logger.Debug(
					"asr stage completed",
					"duration", processed.ASRDuration.Round(time.Millisecond),
					"transcript_chars", len([]rune(processed.Transcript)),
					"warning", blankIfEmpty(processed.TranscriptWarning, "none"),
				)
				logger.Debug(
					"correction stage completed",
					"duration", processed.CorrectionDuration.Round(time.Millisecond),
					"corrected_chars", len([]rune(processed.Corrected)),
					"changed", processed.Corrected != "" && processed.Corrected != processed.Transcript,
					"warning", blankIfEmpty(processed.CorrectionWarning, "none"),
				)
				logger.Debug(
					"output stage completed",
					"duration", processed.OutputDuration.Round(time.Millisecond),
					"clipboard", processed.Output.ClipboardWritten,
					"clipboard_method", blankIfEmpty(processed.Output.ClipboardMethod, "none"),
					"clipboard_duration", processed.Output.ClipboardDuration.Round(time.Millisecond),
					"clipboard_warning", blankIfEmpty(processed.Output.ClipboardWarning, "none"),
					"paste", processed.Output.PasteExecuted,
					"paste_method", blankIfEmpty(processed.Output.PasteMethod, "none"),
					"paste_shortcut", blankIfEmpty(processed.Output.PasteShortcut, "none"),
					"paste_target", blankIfEmpty(processed.Output.PasteTarget, "unknown"),
					"paste_duration", processed.Output.PasteDuration.Round(time.Millisecond),
					"paste_warning", blankIfEmpty(processed.Output.PasteWarning, "none"),
				)
				logger.Debug(
					"pipeline stage timings",
					"asr_duration", processed.ASRDuration.Round(time.Millisecond),
					"correction_duration", processed.CorrectionDuration.Round(time.Millisecond),
					"output_duration", processed.OutputDuration.Round(time.Millisecond),
					"total_duration", processed.TotalDuration.Round(time.Millisecond),
				)
				if processed.TranscriptWarning != "" {
					logger.Warn("transcript warning", "warning", processed.TranscriptWarning)
				}
				if processed.CorrectionWarning != "" {
					logger.Warn("correction warning", "warning", processed.CorrectionWarning)
				}
				logger.Info(
					"output result",
					"clipboard", processed.Output.ClipboardWritten,
					"clipboard_method", blankIfEmpty(processed.Output.ClipboardMethod, "none"),
					"clipboard_duration", processed.Output.ClipboardDuration.Round(time.Millisecond),
					"paste", processed.Output.PasteExecuted,
					"paste_method", blankIfEmpty(processed.Output.PasteMethod, "none"),
					"paste_shortcut", blankIfEmpty(processed.Output.PasteShortcut, "none"),
					"paste_target", blankIfEmpty(processed.Output.PasteTarget, "unknown"),
					"paste_duration", processed.Output.PasteDuration.Round(time.Millisecond),
				)
				logger.Debug(
					"output delivery details",
					"clipboard_method", blankIfEmpty(processed.Output.ClipboardMethod, "none"),
					"clipboard_duration", processed.Output.ClipboardDuration.Round(time.Millisecond),
					"clipboard_warning", blankIfEmpty(processed.Output.ClipboardWarning, "none"),
					"paste_method", blankIfEmpty(processed.Output.PasteMethod, "none"),
					"paste_shortcut", blankIfEmpty(processed.Output.PasteShortcut, "none"),
					"paste_target", blankIfEmpty(processed.Output.PasteTarget, "unknown"),
					"paste_duration", processed.Output.PasteDuration.Round(time.Millisecond),
					"paste_warning", blankIfEmpty(processed.Output.PasteWarning, "none"),
				)
				if processed.Output.PasteWarning != "" {
					logger.Warn("paste warning", "warning", processed.Output.PasteWarning)
				}
				a.emitNotification(logger, a.notificationForProcessing(processed))
			default:
				logger.Warn("unknown trigger event", "type", event.Type)
			}
		}
	}
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func processedActivityPreview(result audio.Result) string {
	activity := audio.AnalyzeActivity(result, audio.DefaultActivityThresholds())
	if !activity.Supported {
		return ""
	}
	return activity.Summary()
}

func (a *App) handleControl(_ context.Context, req control.Request) control.Response {
	if a.ExternalHotkey == nil {
		return control.Response{
			OK:      false,
			Message: "external trigger fallback is not active in this runtime",
		}
	}

	switch req.Command {
	case control.CommandPing:
		return control.Response{OK: true, Message: "pong", Active: a.ExternalHotkey.Active()}
	case control.CommandTriggerStart:
		changed := a.ExternalHotkey.TriggerStart()
		return control.Response{
			OK:      true,
			Message: pickMessage(changed, "trigger started", "trigger already active"),
			Active:  a.ExternalHotkey.Active(),
		}
	case control.CommandTriggerStop:
		changed := a.ExternalHotkey.TriggerStop()
		return control.Response{
			OK:      true,
			Message: pickMessage(changed, "trigger stopped", "trigger already inactive"),
			Active:  a.ExternalHotkey.Active(),
		}
	case control.CommandTriggerToggle:
		active := a.ExternalHotkey.TriggerToggle()
		return control.Response{
			OK:      true,
			Message: pickMessage(active, "trigger toggled on", "trigger toggled off"),
			Active:  active,
		}
	case control.CommandTriggerStatus:
		active := a.ExternalHotkey.Active()
		return control.Response{
			OK:      true,
			Message: pickMessage(active, "trigger active", "trigger inactive"),
			Active:  active,
		}
	default:
		return control.Response{
			OK:      false,
			Message: fmt.Sprintf("unsupported control command %q", req.Command),
			Active:  a.ExternalHotkey.Active(),
		}
	}
}

func describeFeature(mode, detail string) string {
	if detail == "" {
		return mode
	}
	return fmt.Sprintf("%s (%s)", mode, detail)
}

func pickMessage(condition bool, yes, no string) string {
	if condition {
		return yes
	}
	return no
}

func blankIfEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
