// Package term — SP0.5-A: local structured attach for managed sessions over
// the privileged 0600 socket. The stream carries ONLY projected bounded
// ManagedEvent JSONL lines (never raw JSON-RPC, PTY bytes, or prompts echoed
// back); the client sends bounded single-line UTF-8 prompts as
// {"prompt":"..."} JSON lines. Client EOF detaches the viewer only — it never
// affects the runtime.
package term

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// validateManagedPrompt enforces the SP0.5 prompt bounds: non-empty, valid
// UTF-8, single line, no control bytes, ≤ managedPromptMaxBytes. Anything
// else fails closed before any provider write.
func validateManagedPrompt(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("prompt is empty")
	}
	if len(text) > managedPromptMaxBytes {
		return fmt.Errorf("prompt exceeds %d bytes", managedPromptMaxBytes)
	}
	if !utf8.ValidString(text) {
		return fmt.Errorf("prompt is not valid UTF-8")
	}
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("prompt contains control characters")
		}
	}
	return nil
}

// managedAttachLine is one client→daemon line on an attached connection.
// Field set is closed: only a prompt.
type managedAttachLine struct {
	Prompt string `json:"prompt"`
}

// handleManagedAttach bridges one local client to a managed session:
// projected events stream out as JSONL from the given cursor; prompt lines
// come in and are delivered ONLY through ManagedCodexService.SubmitPrompt.
func handleManagedAttach(conn net.Conn, reader *bufio.Reader, managed *ManagedCodexService, sessionID string, cursor uint64, deviceID string, deviceEpoch uint64) {
	enc := json.NewEncoder(conn)
	var wmu sync.Mutex
	writeLine := func(v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return enc.Encode(v)
	}
	if managed == nil {
		_ = writeLine(map[string]string{"error": "managed codex runtime unavailable: daemon started without --enable-managed-codex"})
		return
	}
	store, epoch, ok := managed.eventStoreFor(sessionID)
	if !ok {
		_ = writeLine(map[string]string{"error": "managed session not found"})
		return
	}

	// Writer: stream events after cursor; wake on append; stop when the
	// connection or the store closes.
	writerStop := make(chan struct{})
	go func() {
		defer conn.Close()
		sub := store.subscribe()
		defer store.unsubscribe(sub)
		cur := cursor
		for {
			events, err := store.readAfter(cur, 64)
			if err != nil {
				_ = writeLine(map[string]string{"error": err.Error()})
				return
			}
			for _, ev := range events {
				if err := writeLine(ev); err != nil {
					return
				}
				cur = ev.Seq
			}
			select {
			case <-sub:
			case <-writerStop:
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}()
	defer close(writerStop)

	// Reader: bounded prompt lines (a line beyond the prompt bound + framing
	// margin fails the connection closed instead of growing unbounded). EOF/
	// close detaches the viewer only.
	sc := bufio.NewScanner(reader)
	sc.Buffer(make([]byte, 4096), managedPromptMaxBytes+1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var in managedAttachLine
		if jerr := json.Unmarshal([]byte(line), &in); jerr != nil {
			_ = writeLine(map[string]string{"error": "malformed input line"})
			continue
		}
		if perr := managed.SubmitPrompt(sessionID, epoch, in.Prompt, deviceID, deviceEpoch); perr != nil {
			_ = writeLine(map[string]string{"error": perr.Error()})
		}
	}
	if sc.Err() != nil {
		_ = writeLine(map[string]string{"error": "input line exceeds bound"})
	}
}
