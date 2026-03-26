package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

const envSocketPath = "COE_CONTROL_SOCKET"

type Command string

const (
	CommandPing          Command = "ping"
	CommandTriggerStart  Command = "trigger.start"
	CommandTriggerStop   Command = "trigger.stop"
	CommandTriggerToggle Command = "trigger.toggle"
	CommandTriggerStatus Command = "trigger.status"
)

type Request struct {
	Command Command `json:"command"`
}

type Response struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Active  bool   `json:"active"`
}

type Handler func(context.Context, Request) Response

type Server struct {
	socketPath string
	listener   net.Listener
	handler    Handler
}

func ResolveSocketPath() (string, error) {
	if path := os.Getenv(envSocketPath); path != "" {
		return path, nil
	}

	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "coe", "control.sock"), nil
	}

	return filepath.Join(os.TempDir(), fmt.Sprintf("coe-%d.sock", os.Getuid())), nil
}

func NewServer(socketPath string, handler Handler) (*Server, error) {
	if err := prepareSocket(socketPath); err != nil {
		return nil, err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	return &Server{
		socketPath: socketPath,
		listener:   listener,
		handler:    handler,
	}, nil
}

func (s *Server) SocketPath() string {
	return s.socketPath
}

func (s *Server) Serve(ctx context.Context) error {
	defer os.Remove(s.socketPath)

	go func() {
		<-ctx.Done()
		_ = s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}

		go s.handleConn(ctx, conn)
	}
}

func Send(ctx context.Context, socketPath string, req Request) (Response, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return Response{}, err
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}

	return resp, nil
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{
			OK:      false,
			Message: fmt.Sprintf("decode request: %v", err),
		})
		return
	}

	resp := s.handler(ctx, req)
	_ = json.NewEncoder(conn).Encode(resp)
}

func prepareSocket(socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return err
	}

	info, err := os.Stat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("socket path %s is a directory", socketPath)
	}

	conn, err := net.DialTimeout("unix", socketPath, 150*time.Millisecond)
	if err == nil {
		conn.Close()
		return fmt.Errorf("control socket already in use at %s", socketPath)
	}

	return os.Remove(socketPath)
}
