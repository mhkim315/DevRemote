package mux

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestSessionE2E(t *testing.T) {
	// Create a new session with a dummy command
	sessionID := "test-session-1"
	s, err := NewSession(sessionID, "sh")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Make sure session is retrievable
	retrieved, ok := GetSession(sessionID)
	if !ok || retrieved != s {
		t.Fatalf("Expected to retrieve the same session")
	}

	// Register a listener for PTY output
	ch := make(chan []byte, 10)
	s.AddListener(ch)

	// Send a command to the PTY
	testCmd := "echo 'hello test world'\n"
	_, err = s.Write([]byte(testCmd))
	if err != nil {
		t.Fatalf("Failed to write to session: %v", err)
	}

	// Read from the listener to verify the output is captured
	var outputBuffer bytes.Buffer
	timeout := time.After(2 * time.Second)
	
	outputMatched := false
	for {
		select {
		case data := <-ch:
			outputBuffer.Write(data)
			if strings.Contains(outputBuffer.String(), "hello test world") {
				outputMatched = true
				break
			}
		case <-timeout:
			break
		}
		if outputMatched {
			break
		}
	}

	if !outputMatched {
		t.Fatalf("Failed to receive expected output from session listener. Got: %q", outputBuffer.String())
	}

	// Verify screen capture has the output as well
	time.Sleep(100 * time.Millisecond) // Allow vt100 emulator to process
	screen := s.CaptureScreen()
	if !strings.Contains(screen, "hello test world") {
		t.Fatalf("Failed to find expected output in screen capture. Got: %q", screen)
	}

	// Clean up
	s.Cmd.Process.Kill()
	s.PTY.Close() // this should terminate the shell and the session

	// Verify session is removed
	time.Sleep(500 * time.Millisecond) // Allow cleanup goroutine to run
	_, ok = GetSession(sessionID)
	if ok {
		t.Fatalf("Expected session to be removed after PTY close")
	}
}
