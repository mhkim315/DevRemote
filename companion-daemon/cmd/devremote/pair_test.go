package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"rsc.io/qr"
)

func TestRenderQRANSICleanPerRow(t *testing.T) {
	payload := map[string]string{
		"sessionId": "test-session", "hostId": "test-host",
		"fingerprint": "aa", "hostPubKey": "bb",
		"bootstrapToken": "cc", "endpoint": "http://x",
		"expiresAt": "2026-01-01T00:00:00Z",
	}
	data, _ := json.Marshal(payload)
	code, err := qr.Encode(string(data), qr.M)
	if err != nil {
		t.Fatalf("qr.Encode: %v", err)
	}

	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf []byte
		b := make([]byte, 8192)
		for {
			n, err := r.Read(b)
			if n > 0 {
				buf = append(buf, b[:n]...)
			}
			if err != nil {
				break
			}
		}
		done <- string(buf)
	}()
	renderQRANSI(code, 4, 1)
	w.Close()
	os.Stdout = old
	out := <-done

	lines := strings.Split(out, "\n")
	renderLines := 0
	for _, line := range lines {
		if len(line) == 0 || !strings.Contains(line, "\033[") {
			continue
		}
		renderLines++
		if !strings.HasPrefix(line, "\033[0m") {
			t.Errorf("line %d does not start with ANSI reset: %q", renderLines, line[:min(40, len(line))])
		}
		if !strings.HasSuffix(line, "\033[0m") {
			t.Errorf("line %d does not end with ANSI reset: %q", renderLines, line[max(0, len(line)-40):])
		}
		lastReset := strings.LastIndex(line, "\033[0m")
		tail := line[lastReset+4:]
		if len(tail) > 0 {
			t.Errorf("line %d has %d bytes after final ANSI reset: %q", renderLines, len(tail), tail)
		}
	}
	if renderLines == 0 {
		t.Fatal("no ANSI-rendered lines in output")
	}
	t.Logf("%d ANSI-rendered lines all start and end with reset", renderLines)
}
