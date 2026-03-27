package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"coe/internal/asr"
	"coe/internal/audio"
	"coe/internal/capabilities"
	"coe/internal/config"
	"coe/internal/control"
	"coe/internal/focus"
	"coe/internal/hotkey"
	dbusipc "coe/internal/ipc/dbus"
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
	DictationBus      *dbusipc.Service
	resourceClosers   []io.Closer
	dictationState    *dictationState
	runtimeCommands   chan runtimeCommand
	runtimeRunning    atomic.Bool
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	cfg.Runtime.Mode = config.NormalizeRuntimeMode(cfg.Runtime.Mode)
	if !config.IsSupportedRuntimeMode(cfg.Runtime.Mode) {
		return nil, fmt.Errorf("unsupported runtime.mode %q", cfg.Runtime.Mode)
	}

	caps, err := capabilities.Probe(ctx)
	if err != nil {
		return nil, err
	}
	desktopRuntime := cfg.Runtime.Mode == config.RuntimeModeDesktop

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
	if desktopRuntime && cfg.Output.PersistPortalAccess && caps.Portals.RemoteDesktop.Version >= 2 {
		statePath, err := state.ResolvePath()
		if err != nil {
			return nil, err
		}
		portalStateStore = state.NewStore(statePath)
	}

	description := describeFeature(string(caps.Hotkey.Mode), caps.Hotkey.Detail)
	if cfg.Runtime.Mode == config.RuntimeModeFcitx {
		description = "fcitx module over D-Bus"
	}
	service := hotkey.Service(hotkey.PlannedService{Description: description})
	var external *hotkey.ExternalTriggerService
	var controlSocketPath string
	startupWarnings := make([]string, 0, 2)

	if cfg.Runtime.TargetDesktop == "gnome" {
		manager := gnome.NewShortcutManager()
		if desktopRuntime {
			if caps.Hotkey.Mode == capabilities.ModeExternalBinding {
				external = hotkey.NewExternalTriggerService(description)
				service = external

				socketPath, err := control.ResolveSocketPath()
				if err != nil {
					return nil, err
				}
				controlSocketPath = socketPath

				if err := manager.EnsureTriggerShortcut(ctx, cfg.Hotkey.Name, cfg.Hotkey.PreferredAccelerator); err != nil {
					startupWarnings = append(startupWarnings, fmt.Sprintf("GNOME custom shortcut setup failed: %v", err))
				}
			}
		} else if cfg.Runtime.Mode == config.RuntimeModeFcitx {
			if err := manager.RemoveTriggerShortcut(ctx, cfg.Hotkey.Name); err != nil {
				startupWarnings = append(startupWarnings, fmt.Sprintf("GNOME custom shortcut cleanup failed: %v", err))
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
	if desktopRuntime && cfg.Output.UseGNOMEFocusHelper && cfg.Runtime.TargetDesktop == "gnome" {
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
		dictationState:    newDictationState(),
		runtimeCommands:   make(chan runtimeCommand, 16),
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

	dictationBus, err := dbusipc.ConnectSession(instance)
	if err != nil {
		instance.StartupWarnings = append(instance.StartupWarnings, fmt.Sprintf("dictation D-Bus service unavailable: %v", err))
	} else {
		instance.DictationBus = dictationBus
		instance.resourceClosers = append(instance.resourceClosers, dictationBus)
	}

	return instance, nil
}

func (a *App) Serve(ctx context.Context, w io.Writer) error {
	a.runtimeRunning.Store(true)
	defer a.runtimeRunning.Store(false)

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
		"mode", a.Config.Runtime.Mode,
		"hotkey", a.Hotkey.Plan(),
		"pipeline", a.Pipeline.Summary(),
		"notifications", blankIfEmpty(a.Notifier.Summary(), "disabled"),
		"dictation_dbus", a.DictationBus != nil,
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
	var captureSource string

	handleStart := func(source string) runtimeCommandResponse {
		if captureSession != nil {
			return runtimeCommandResponse{Active: true}
		}

		session, err := a.Pipeline.Recorder.Start(ctx)
		if err != nil {
			message := fmt.Sprintf("recording start failed: %v", err)
			status := a.dictationState.Error(message)
			a.emitStateChanged(logger, status)
			a.emitDictationError(logger, status.SessionID, message)
			logger.Error("recording start failed", "error", err, "source", source)
			a.emitNotification(logger, notificationForFailure("Recording failed to start", err))
			return runtimeCommandResponse{Err: err}
		}

		captureSession = session
		captureSource = source
		a.emitStateChanged(logger, a.dictationState.Recording("recording started"))
		logger.Info("recording started", "source", source)
		a.emitNotification(logger, a.notificationForStart())
		return runtimeCommandResponse{Active: true, Changed: true}
	}

	handleStop := func(source string) runtimeCommandResponse {
		if captureSession == nil {
			return runtimeCommandResponse{}
		}

		effectiveSource := captureSource
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		result, err := captureSession.Stop(stopCtx)
		cancel()
		captureSession = nil
		captureSource = ""
		if err != nil && result.ByteCount == 0 {
			message := fmt.Sprintf("recording stop failed: %v", err)
			status := a.dictationState.Error(message)
			a.emitStateChanged(logger, status)
			a.emitDictationError(logger, status.SessionID, message)
			stopAttrs := []any{"error", err, "source", effectiveSource}
			if result.Stderr != "" {
				stopAttrs = append(stopAttrs, "stderr", result.Stderr)
			}
			logger.Error("recording stop failed", stopAttrs...)
			a.emitNotification(logger, notificationForFailure("Recording failed", err))
			return runtimeCommandResponse{Err: err}
		}
		if err != nil {
			logger.Warn("recording stop returned warning", "error", err, "source", effectiveSource)
		}

		a.emitStateChanged(logger, a.dictationState.Processing("processing audio"))
		recordingAttrs := []any{
			"bytes", result.ByteCount,
			"duration", result.Duration.Round(time.Millisecond),
			"source", effectiveSource,
		}
		activity := processedActivityPreview(result)
		if activity != "" {
			recordingAttrs = append(recordingAttrs, "audio_activity", activity)
		}
		if result.Stderr != "" {
			recordingAttrs = append(recordingAttrs, "stderr", result.Stderr)
		}
		logger.Info("recording stopped", recordingAttrs...)
		logger.Debug("capture processing started", "bytes", result.ByteCount, "source", effectiveSource)

		processor := a.Pipeline
		if effectiveSource == "fcitx-module" {
			processor.Output = nil
		}
		processed, err := processor.ProcessCapture(ctx, result)
		if err != nil {
			status := a.dictationState.Error(err.Error())
			a.emitStateChanged(logger, status)
			a.emitDictationError(logger, status.SessionID, err.Error())
			logger.Error("pipeline processing failed", "error", err, "source", effectiveSource)
			a.emitNotification(logger, notificationForFailure("Dictation failed", err))
			return runtimeCommandResponse{Err: err}
		}

		pipelineAttrs := []any{
			"transcript", processed.Transcript,
			"corrected", processed.Corrected,
			"asr_duration", processed.ASRDuration.Round(time.Millisecond),
			"correction_duration", processed.CorrectionDuration.Round(time.Millisecond),
			"output_duration", processed.OutputDuration.Round(time.Millisecond),
			"total_duration", processed.TotalDuration.Round(time.Millisecond),
			"source", effectiveSource,
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
		if strings.TrimSpace(processed.Corrected) == "" {
			message := firstNonEmpty(
				processed.TranscriptWarning,
				processed.CorrectionWarning,
				"dictation produced no text",
			)
			status := a.dictationState.Error(message)
			a.emitStateChanged(logger, status)
			a.emitDictationError(logger, status.SessionID, message)
		} else {
			status := a.dictationState.Completed("result ready")
			a.emitStateChanged(logger, status)
			a.emitResultReady(logger, status.SessionID, processed.Corrected)
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
		a.emitNotification(logger, a.notificationForProcessing(processed, effectiveSource))
		return runtimeCommandResponse{Changed: true}
	}

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
		case command := <-a.runtimeCommands:
			var response runtimeCommandResponse
			switch command.Type {
			case runtimeCommandToggle:
				if captureSession == nil {
					response = handleStart(command.Source)
				} else {
					response = handleStop(command.Source)
				}
			case runtimeCommandStart:
				response = handleStart(command.Source)
			case runtimeCommandStop:
				response = handleStop(command.Source)
			default:
				response = runtimeCommandResponse{Err: fmt.Errorf("unsupported runtime command %q", command.Type)}
			}
			command.Reply <- response
		case event, ok := <-events:
			if !ok {
				logger.Info("hotkey service stopped")
				return nil
			}
			switch event.Type {
			case hotkey.Activated:
				_ = handleStart("hotkey")
			case hotkey.Deactivated:
				_ = handleStop("hotkey")
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
	switch req.Command {
	case control.CommandPing:
		active := a.triggerActive()
		return control.Response{OK: true, Message: "pong", Active: active}
	case control.CommandTriggerStart:
		changed, err := a.triggerStart()
		if err != nil {
			return control.Response{OK: false, Message: err.Error()}
		}
		return control.Response{
			OK:      true,
			Message: pickMessage(changed, "trigger started", "trigger already active"),
			Active:  a.triggerActive(),
		}
	case control.CommandTriggerStop:
		changed, err := a.triggerStop()
		if err != nil {
			return control.Response{OK: false, Message: err.Error()}
		}
		return control.Response{
			OK:      true,
			Message: pickMessage(changed, "trigger stopped", "trigger already inactive"),
			Active:  a.triggerActive(),
		}
	case control.CommandTriggerToggle:
		active, err := a.triggerToggle()
		if err != nil {
			return control.Response{OK: false, Message: err.Error()}
		}
		return control.Response{
			OK:      true,
			Message: pickMessage(active, "trigger toggled on", "trigger toggled off"),
			Active:  active,
		}
	case control.CommandTriggerStatus:
		active := a.triggerActive()
		return control.Response{
			OK:      true,
			Message: pickMessage(active, "trigger active", "trigger inactive"),
			Active:  active,
		}
	default:
		return control.Response{
			OK:      false,
			Message: fmt.Sprintf("unsupported control command %q", req.Command),
			Active:  a.triggerActive(),
		}
	}
}

func (a *App) Toggle(context.Context) error {
	_, err := a.triggerToggleFrom("fcitx-module")
	return err
}

func (a *App) Start(context.Context) error {
	_, err := a.triggerStartFrom("fcitx-module")
	return err
}

func (a *App) Stop(context.Context) error {
	_, err := a.triggerStopFrom("fcitx-module")
	return err
}

func (a *App) Status(context.Context) dbusipc.Status {
	if a.dictationState == nil {
		return dbusipc.Status{State: "idle"}
	}
	return a.dictationState.Snapshot()
}

func (a *App) triggerToggle() (bool, error) {
	return a.triggerToggleFrom("ipc")
}

func (a *App) triggerToggleFrom(source string) (bool, error) {
	response, err := a.executeRuntimeCommand(context.Background(), runtimeCommandToggle, source)
	if err != nil {
		return false, err
	}
	return response.Active, nil
}

func (a *App) triggerStart() (bool, error) {
	return a.triggerStartFrom("ipc")
}

func (a *App) triggerStartFrom(source string) (bool, error) {
	response, err := a.executeRuntimeCommand(context.Background(), runtimeCommandStart, source)
	if err != nil {
		return false, err
	}
	return response.Changed, nil
}

func (a *App) triggerStop() (bool, error) {
	return a.triggerStopFrom("ipc")
}

func (a *App) triggerStopFrom(source string) (bool, error) {
	response, err := a.executeRuntimeCommand(context.Background(), runtimeCommandStop, source)
	if err != nil {
		return false, err
	}
	return response.Changed, nil
}

func (a *App) triggerActive() bool {
	status := a.Status(context.Background())
	return status.State == "recording"
}

func (a *App) emitStateChanged(logger *slog.Logger, status dbusipc.Status) {
	if a.DictationBus == nil {
		return
	}
	if err := a.DictationBus.EmitStateChanged(status); err != nil {
		logger.Warn("dictation D-Bus state emit failed", "error", err)
	}
}

func (a *App) emitResultReady(logger *slog.Logger, sessionID, text string) {
	if a.DictationBus == nil {
		return
	}
	if err := a.DictationBus.EmitResultReady(sessionID, text); err != nil {
		logger.Warn("dictation D-Bus result emit failed", "error", err)
	}
}

func (a *App) emitDictationError(logger *slog.Logger, sessionID, message string) {
	if a.DictationBus == nil {
		return
	}
	if err := a.DictationBus.EmitError(sessionID, message); err != nil {
		logger.Warn("dictation D-Bus error emit failed", "error", err)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (a *App) executeRuntimeCommand(ctx context.Context, kind runtimeCommandType, source string) (runtimeCommandResponse, error) {
	if !a.runtimeRunning.Load() {
		return runtimeCommandResponse{}, errors.New("dictation runtime loop is not ready")
	}

	command := runtimeCommand{
		Type:   kind,
		Source: source,
		Reply:  make(chan runtimeCommandResponse, 1),
	}

	select {
	case a.runtimeCommands <- command:
	case <-ctx.Done():
		return runtimeCommandResponse{}, ctx.Err()
	}

	select {
	case response := <-command.Reply:
		if response.Err != nil {
			return response, response.Err
		}
		return response, nil
	case <-ctx.Done():
		return runtimeCommandResponse{}, ctx.Err()
	}
}
