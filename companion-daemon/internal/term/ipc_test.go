package term

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

func TestIPCServer_Lifecycle(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")
	reg := mux.MustNewRegistry()

	srv, err := StartIPCServer(socketPath, reg, NewMemoryEventStore(), NewNopLinkStore(), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("StartIPCServer failed: %v", err)
	}

	// Verify we can connect.
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to connect to IPC socket: %v", err)
	}
	conn.Close()

	// Close the server and wait for accept loop to exit.
	if err := srv.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Wait(ctx); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
}

func TestIPCServer_CloseIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")
	reg := mux.MustNewRegistry()

	srv, err := StartIPCServer(socketPath, reg, NewMemoryEventStore(), NewNopLinkStore(), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("StartIPCServer failed: %v", err)
	}

	// Close twice — must not panic or hang.
	if err := srv.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := srv.Wait(ctx); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
}

func TestIPCServer_SocketMode0600(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")
	reg := mux.MustNewRegistry()

	srv, err := StartIPCServer(socketPath, reg, NewMemoryEventStore(), NewNopLinkStore(), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("StartIPCServer failed: %v", err)
	}
	defer func() {
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Wait(ctx)
	}()

	fi, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("socket mode = %o, want 0600", fi.Mode().Perm())
	}
}

func TestIPCServer_RebindAfterClose(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")
	reg := mux.MustNewRegistry()

	srv1, err := StartIPCServer(socketPath, reg, NewMemoryEventStore(), NewNopLinkStore(), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first StartIPCServer failed: %v", err)
	}
	srv1.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv1.Wait(ctx)

	// Remove the socket so we can rebind.
	os.Remove(socketPath)

	srv2, err := StartIPCServer(socketPath, reg, NewMemoryEventStore(), NewNopLinkStore(), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("second StartIPCServer failed: %v", err)
	}
	srv2.Close()
	srv2.Wait(ctx)
}
