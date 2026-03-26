package control

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSendAndServe(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "control.sock")
	server, err := NewServer(socketPath, func(_ context.Context, req Request) Response {
		return Response{
			OK:      true,
			Message: string(req.Command),
			Active:  req.Command == CommandTriggerToggle,
		}
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ctx)
	}()

	time.Sleep(25 * time.Millisecond)

	resp, err := Send(context.Background(), socketPath, Request{Command: CommandTriggerToggle})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !resp.OK {
		t.Fatal("expected OK response")
	}
	if !resp.Active {
		t.Fatal("expected Active to be true")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}
