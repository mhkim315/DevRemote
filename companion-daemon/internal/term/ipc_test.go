package term

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIPCServer_Lifecycle(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	srv, err := StartIPCServer(socketPath, nil, nil, nil, nil)
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

// TestPA2a_IPCLinkOperationsRemoved proves that the removed link, unlink,
// and links IPC operations fail through the existing unsupported-operation
// behavior without panicking or blocking.
func TestPA2a_IPCLinkOperationsRemoved(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join("/tmp", fmt.Sprintf("pokit-pa2a-%d.sock", time.Now().UnixNano()))

	srv, err := StartIPCServer(socketPath, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	defer os.Remove(socketPath)
	defer func() {
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Wait(ctx)
	}()

	for _, op := range []string{"link", "unlink", "links"} {
		conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
		if err != nil {
			t.Fatalf("dial for %q: %v", op, err)
		}
		// The create operation is the only recognized JSON operation after
		// link removal. Sending link/unlink/links hits the "else" branch,
		// which produces no output or an error — it must not panic or block.
		req := fmt.Sprintf(`{"operation":"%s","sessionId":"x"}`, op)
		conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		if _, err := conn.Write([]byte(req + "\n")); err != nil {
			conn.Close()
			t.Fatalf("write for %q: %v", op, err)
		}
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		buf := make([]byte, 256)
		n, readErr := conn.Read(buf)
		conn.Close()
		if readErr != nil {
			if strings.Contains(readErr.Error(), "timeout") || strings.Contains(readErr.Error(), "deadline") {
				t.Errorf("%q: read timed out (handler may have blocked)", op)
			} else {
				t.Errorf("%q: read error: %v", op, readErr)
			}
			continue
		}
		response := string(buf[:n])
		expected := "unknown operation\n"
		if response != expected {
			t.Errorf("%q: got %q, want %q", op, response, expected)
		}
	}
}

func TestIPCServer_CloseIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	srv, err := StartIPCServer(socketPath, nil, nil, nil, nil)
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

	srv, err := StartIPCServer(socketPath, nil, nil, nil, nil)
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

	srv1, err := StartIPCServer(socketPath, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first StartIPCServer failed: %v", err)
	}
	srv1.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv1.Wait(ctx)

	// Remove the socket so we can rebind.
	os.Remove(socketPath)

	srv2, err := StartIPCServer(socketPath, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("second StartIPCServer failed: %v", err)
	}
	srv2.Close()
	srv2.Wait(ctx)
}
