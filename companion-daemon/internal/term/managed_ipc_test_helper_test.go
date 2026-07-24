package term

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"testing"
)

// ipcRoundTripWith drives the JSON IPC boundary without a legacy registry.
func ipcRoundTripWith(t *testing.T, managed *ManagedCodexService, body map[string]any) map[string]string {
	t.Helper()
	client, server := net.Pipe()
	go handleIPCConnection(server, testMutationAuthorizer{}, nil, nil, managed, nil)
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(client).ReadString('\n')
	client.Close()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(line), &out); err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	return out
}
