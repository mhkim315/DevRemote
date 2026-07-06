package mux

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockCmuxRunner struct {
	runFunc func(ctx context.Context, opts CommandOptions, args ...string) ([]byte, error)
}

func (m *mockCmuxRunner) Run(ctx context.Context, opts CommandOptions, args ...string) ([]byte, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, opts, args...)
	}
	return nil, nil
}

func TestPollScreenFailures(t *testing.T) {
	var calls int

	mockRunner := &mockCmuxRunner{
		runFunc: func(ctx context.Context, opts CommandOptions, args ...string) ([]byte, error) {
			if args[0] == "read-screen" {
				calls++
				// First poll (call 1) succeeds, polling fails after
				if calls > 1 {
					return nil, &CmuxError{Args: args, ExitCode: 1, Stderr: "connection refused"}
				}
				return []byte("test frame"), nil
			}
			return nil, nil
		},
	}

	session := &CmuxSession{
		id:        "surface:1",
		surfaceID: "surface:1",
		runner:    mockRunner,
	}

	stream, err := session.OpenStream(context.Background())
	if err != nil {
		t.Fatalf("OpenStream failed: %v", err)
	}

	buf := make([]byte, 1024)

	// Read the first frame which succeeds
	n, readErr := stream.Read(buf)
	if readErr != nil {
		t.Fatalf("Expected first frame read to succeed, got err: %v", readErr)
	}
	if string(buf[:n]) != "\033[2J\033[Htest frame" {
		t.Fatalf("Unexpected first frame content: %q", string(buf[:n]))
	}

	done := make(chan error)
	go func() {
		_, err := stream.Read(buf)
		done <- err
	}()

	select {
	case readErr := <-done:
		if readErr == nil {
			t.Fatalf("expected error from stream read, got nil")
		}
		if !strings.Contains(readErr.Error(), "read-screen failed 3 times") {
			t.Fatalf("expected max errors message, got: %v", readErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for stream to close due to max errors")
	}
}

func TestPollScreenBlockedWriteClose(t *testing.T) {
	mockRunner := &mockCmuxRunner{
		runFunc: func(ctx context.Context, opts CommandOptions, args ...string) ([]byte, error) {
			if args[0] == "read-screen" {
				return []byte("frame data"), nil
			}
			return nil, nil
		},
	}

	session := &CmuxSession{
		id:        "surface:1",
		surfaceID: "surface:1",
		runner:    mockRunner,
	}

	stream, err := session.OpenStream(context.Background())
	if err != nil {
		t.Fatalf("OpenStream failed: %v", err)
	}

	// We don't read from stream, so the second pollOnce will block on pw.Write.
	// Wait a bit to let it block
	time.Sleep(1 * time.Second)

	// Close stream while blocked
	if err := stream.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Assert that we can exit cleanly
	// Check read returns EOF or closed pipe
	buf := make([]byte, 1024)
	_, readErr := stream.Read(buf)
	if readErr == nil {
		// First frame is already in pipe buffer from OpenStream, so read might succeed once.
		// Let's read until EOF
		for {
			_, err := stream.Read(buf)
			if err != nil {
				break
			}
		}
	}
}

func TestPollScreenCloseCancelsInFlightCommand(t *testing.T) {
	commandStarted := make(chan struct{})
	commandCanceled := make(chan struct{})
	var calls int

	mockRunner := &mockCmuxRunner{
		runFunc: func(ctx context.Context, opts CommandOptions, args ...string) ([]byte, error) {
			calls++
			if calls == 1 {
				return []byte("initial frame"), nil
			}
			close(commandStarted)
			<-ctx.Done()
			close(commandCanceled)
			return nil, ctx.Err()
		},
	}

	session := &CmuxSession{
		id:        "surface:1",
		surfaceID: "surface:1",
		runner:    mockRunner,
	}
	stream, err := session.OpenStream(context.Background())
	if err != nil {
		t.Fatalf("OpenStream failed: %v", err)
	}

	buf := make([]byte, 1024)
	if _, err := stream.Read(buf); err != nil {
		t.Fatalf("initial frame read failed: %v", err)
	}

	select {
	case <-commandStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for polling command to start")
	}

	if err := stream.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	select {
	case <-commandCanceled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stream close did not cancel the in-flight command")
	}
}

func TestCmuxErrorWithNoArgs(t *testing.T) {
	err := (&CmuxError{ExitCode: -1}).Error()
	if !strings.Contains(err, "cmux command") {
		t.Fatalf("unexpected empty-args error: %q", err)
	}
}

func TestExecRunnerPreservesLookupError(t *testing.T) {
	lookupErr := errors.New("cmux not found")
	runner := &execCommandRunner{binaryPath: "cmux", lookupErr: lookupErr}

	_, err := runner.Run(context.Background(), CommandOptions{}, "tree", "--all")
	var cmuxErr *CmuxError
	if !errors.As(err, &cmuxErr) {
		t.Fatalf("expected CmuxError, got %T: %v", err, err)
	}
	if !errors.Is(cmuxErr, lookupErr) {
		t.Fatalf("expected lookup error to be preserved, got %v", cmuxErr)
	}
	if cmuxErr.ExitCode != -1 {
		t.Fatalf("expected exit code -1, got %d", cmuxErr.ExitCode)
	}
}

func TestSerialCommandRunnerPreventsConcurrentSocketWrites(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	delegate := &mockCmuxRunner{
		runFunc: func(ctx context.Context, opts CommandOptions, args ...string) ([]byte, error) {
			started <- struct{}{}
			<-release
			return nil, nil
		},
	}
	runner := &serialCommandRunner{delegate: delegate}

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _ = runner.Run(context.Background(), CommandOptions{}, "ping")
		}()
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first command did not start")
	}

	select {
	case <-started:
		t.Fatal("second command started before the first released the socket")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	wg.Wait()

	select {
	case <-started:
	default:
		t.Fatal("second command never ran after the first completed")
	}
}
