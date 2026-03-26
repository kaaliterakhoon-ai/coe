package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Result struct {
	Data       []byte
	ByteCount  int
	SampleRate int
	Channels   int
	Format     string
	StartedAt  time.Time
	StoppedAt  time.Time
	Duration   time.Duration
	Stderr     string
}

type CaptureSession interface {
	Stop(context.Context) (Result, error)
}

type Recorder interface {
	Start(context.Context) (CaptureSession, error)
	Summary() string
}

type PWRecord struct {
	Binary     string
	SampleRate int
	Channels   int
	Format     string
}

type pwRecordSession struct {
	cmd       *exec.Cmd
	stdout    io.ReadCloser
	startedAt time.Time

	dataMu sync.Mutex
	data   bytes.Buffer
	stderr bytes.Buffer

	copyErrCh chan error
	waitErrCh chan error

	stopOnce sync.Once
	result   Result
	stopErr  error
}

func (r PWRecord) Start(ctx context.Context) (CaptureSession, error) {
	cmd, err := r.command(ctx)
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	session := &pwRecordSession{
		cmd:       cmd,
		stdout:    stdout,
		startedAt: time.Now(),
		copyErrCh: make(chan error, 1),
		waitErrCh: make(chan error, 1),
	}
	cmd.Stderr = &session.stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() {
		_, err := io.Copy(&session.data, stdout)
		session.copyErrCh <- err
	}()

	go func() {
		session.waitErrCh <- cmd.Wait()
	}()

	return session, nil
}

func (r PWRecord) Summary() string {
	return fmt.Sprintf("%s --rate %d --channels %d --format %s -", r.Binary, r.SampleRate, r.Channels, r.Format)
}

func (r PWRecord) command(ctx context.Context) (*exec.Cmd, error) {
	if r.Binary == "" {
		return nil, fmt.Errorf("pw-record binary is empty")
	}

	cmd := exec.CommandContext(
		ctx,
		r.Binary,
		"--rate", strconv.Itoa(r.SampleRate),
		"--channels", strconv.Itoa(r.Channels),
		"--format", r.Format,
		"-",
	)
	return cmd, nil
}

func (s *pwRecordSession) Stop(ctx context.Context) (Result, error) {
	s.stopOnce.Do(func() {
		stoppedAt := time.Now()
		waitErr := s.stopProcess(ctx)
		copyErr := <-s.copyErrCh

		s.dataMu.Lock()
		data := append([]byte(nil), s.data.Bytes()...)
		s.dataMu.Unlock()

		s.result = Result{
			Data:       data,
			ByteCount:  len(data),
			SampleRate: extractSampleRate(s.cmd.Args),
			Channels:   extractChannels(s.cmd.Args),
			Format:     extractFormat(s.cmd.Args),
			StartedAt:  s.startedAt,
			StoppedAt:  stoppedAt,
			Duration:   stoppedAt.Sub(s.startedAt),
			Stderr:     s.stderr.String(),
		}

		s.stopErr = errors.Join(normalizeStopError(waitErr, s.result), normalizeCopyError(copyErr))
	})

	return s.result, s.stopErr
}

func (s *pwRecordSession) stopProcess(ctx context.Context) error {
	if s.cmd.Process == nil {
		return nil
	}

	_ = s.cmd.Process.Signal(os.Interrupt)

	select {
	case err := <-s.waitErrCh:
		return err
	case <-ctx.Done():
		_ = s.cmd.Process.Kill()
		err := <-s.waitErrCh
		return errors.Join(ctx.Err(), err)
	}
}

func normalizeCopyError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

func normalizeStopError(err error, result Result) error {
	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err
	}

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return err
	}
	if status.Signal() == os.Interrupt || status.Signal() == syscall.SIGTERM {
		return nil
	}
	if exitErr.ExitCode() == 130 {
		return nil
	}
	// PipeWire's pw-record exits 1 when interrupted by SIGINT/SIGTERM unless the
	// stream reaches its drained callback. If we initiated stop, captured audio,
	// and got no stderr, treat that as a successful stop rather than a warning.
	if exitErr.ExitCode() == 1 && result.ByteCount > 0 && strings.TrimSpace(result.Stderr) == "" {
		return nil
	}
	return err
}

func extractSampleRate(args []string) int {
	return extractIntArg(args, "--rate")
}

func extractChannels(args []string) int {
	return extractIntArg(args, "--channels")
}

func extractFormat(args []string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--format" {
			return args[i+1]
		}
	}
	return ""
}

func extractIntArg(args []string, name string) int {
	for i := 0; i < len(args)-1; i++ {
		if args[i] != name {
			continue
		}
		value, err := strconv.Atoi(args[i+1])
		if err == nil {
			return value
		}
	}
	return 0
}
