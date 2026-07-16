package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSocketDaemon accepts one create request on a temp Unix socket, records it,
// and replies with the provided response line.
func fakeSocketDaemon(t *testing.T, reply string) (socketPath string, got *map[string]interface{}) {
	t.Helper()
	// macOS caps Unix socket paths at ~104 bytes, so use a short /tmp dir
	// rather than the long t.TempDir() path.
	dir, err := os.MkdirTemp("/tmp", "pk")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	recv := map[string]interface{}{}
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		json.Unmarshal([]byte(line), &recv)
		conn.Write([]byte(reply))
	}()
	return sock, &recv
}

func TestCreateViaSocket_SendsCreateOpAndParsesID(t *testing.T) {
	sock, got := fakeSocketDaemon(t, `{"id":"controlled_pty:run-9","state":"running"}`)
	id, err := createViaSocketAt(sock, "echo hello", "/tmp")
	if err != nil {
		t.Fatalf("createViaSocketAt: %v", err)
	}
	if id != "controlled_pty:run-9" {
		t.Fatalf("id = %q, want controlled_pty:run-9", id)
	}
	if (*got)["operation"] != "create" {
		t.Fatalf("operation = %v, want create", (*got)["operation"])
	}
	if (*got)["command"] != "echo hello" {
		t.Fatalf("command = %v, want 'echo hello'", (*got)["command"])
	}
	if (*got)["cwd"] != "/tmp" {
		t.Fatalf("cwd = %v, want /tmp", (*got)["cwd"])
	}
}

func TestCreateViaSocket_PropagatesDaemonError(t *testing.T) {
	sock, _ := fakeSocketDaemon(t, `{"error":"command must be a string"}`)
	_, err := createViaSocketAt(sock, "echo hi", "")
	if err == nil || !strings.Contains(err.Error(), "command must be a string") {
		t.Fatalf("err = %v, want daemon error propagated", err)
	}
}

func TestBuildRunCreateRequest_ClaudeProfile(t *testing.T) {
	req := buildRunCreateRequest([]string{"claude"}, "/tmp", true)
	if req["profileId"] != "claude" {
		t.Fatalf("profileId = %v, want claude", req["profileId"])
	}
	if req["command"] != nil {
		t.Fatalf("unexpected legacy command: %v", req["command"])
	}
	if req["detach"] != true {
		t.Fatal("expected detach=true")
	}
}

func TestBuildRunCreateRequest_LegacyFallback(t *testing.T) {
	req := buildRunCreateRequest([]string{"bash", "-c", "echo hi"}, "/tmp", false)
	if req["profileId"] != nil {
		t.Fatalf("unexpected profileId for legacy: %v", req["profileId"])
	}
	if req["command"] != "bash -c echo hi" {
		t.Fatalf("command = %v, want legacy string", req["command"])
	}
}

func TestCreateViaSocket_MissingSocketErrors(t *testing.T) {
	_, err := createViaSocketAt(filepath.Join(t.TempDir(), "nope.sock"), "echo hi", "")
	if err == nil {
		t.Fatal("expected error when socket is missing")
	}
}
