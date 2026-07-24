package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

// TestSecureModeIPCUsesItsOwnLocalAuthority proves that the 0600 IPC trust
// boundary is independent from secure HTTP device authentication. The app is
// composed with a real DeviceRegistry and no test authorizer, yet an empty
// device identity on the local socket reaches the actual create mutation.
func TestSecureModeIPCUsesItsOwnLocalAuthority(t *testing.T) {
	dir := t.TempDir()
	identity, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: filepath.Join(dir, "host.json")})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: filepath.Join(dir, "devices.json")})
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: false}, Dependencies{
		HostIdentity: identity, DeviceRegistry: registry,
	})
	if err != nil {
		t.Fatalf("secure production composition: %v", err)
	}
	app.ipcPath = fmt.Sprintf("/tmp/devremote-ipc-local-%d.sock", os.Getpid())
	ipc, err := app.startIPC()
	if err != nil {
		t.Fatalf("start IPC: %v", err)
	}
	t.Cleanup(func() {
		_ = ipc.Close()
		_ = ipc.Wait(context.Background())
	})

	conn, err := net.DialTimeout("unix", app.ipcPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(map[string]any{
		"version": 1, "operation": "create", "profileId": "custom",
		"name": "secure-ipc-local", "executable": "/bin/sh",
		"args": []string{"-c", "sleep 10"},
	})
	if _, err := conn.Write(append(request, '\n')); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if err != nil {
		t.Fatalf("IPC response: %v", err)
	}
	var response map[string]string
	if err := json.Unmarshal(bytes.TrimSpace([]byte(line)), &response); err != nil {
		t.Fatalf("decode IPC response %q: %v", line, err)
	}
	if response["error"] != "" {
		t.Fatalf("secure IPC local create rejected: %s", response["error"])
	}
	if !strings.HasPrefix(response["id"], "controlled_pty:") {
		t.Fatalf("unexpected IPC response: %v", response)
	}
	if _, err := app.lifecycle.Kill(context.Background(), response["id"], "", 0); err != nil {
		t.Fatalf("local IPC cleanup kill: %v", err)
	}
}
