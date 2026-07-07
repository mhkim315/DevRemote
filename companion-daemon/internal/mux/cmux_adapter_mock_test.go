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

func TestCmuxWriteInputSplitsTextAndLineEndings(t *testing.T) {
	tests := []struct {
		name string
		data string
		want [][]string
	}{
		{
			name: "mobile command with LF",
			data: "echo hello\n",
			want: [][]string{
				{"send", "--surface", "surface:1", "echo hello"},
				{"send-key", "--surface", "surface:1", "enter"},
			},
		},
		{
			name: "CRLF is one enter",
			data: "pwd\r\n",
			want: [][]string{
				{"send", "--surface", "surface:1", "pwd"},
				{"send-key", "--surface", "surface:1", "enter"},
			},
		},
		{
			name: "text after newline is preserved",
			data: "cd /tmp\npwd",
			want: [][]string{
				{"send", "--surface", "surface:1", "cd /tmp"},
				{"send-key", "--surface", "surface:1", "enter"},
				{"send", "--surface", "surface:1", "pwd"},
			},
		},
		{
			name: "repeated LF sends repeated enter",
			data: "\n\n",
			want: [][]string{
				{"send-key", "--surface", "surface:1", "enter"},
				{"send-key", "--surface", "surface:1", "enter"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got [][]string
			runner := &mockCmuxRunner{
				runFunc: func(_ context.Context, _ CommandOptions, args ...string) ([]byte, error) {
					got = append(got, append([]string(nil), args...))
					return nil, nil
				},
			}
			session := &CmuxSession{surfaceID: "surface:1", runner: runner}

			if err := session.WriteInput(context.Background(), []byte(tt.data)); err != nil {
				t.Fatalf("WriteInput failed: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d calls %v, want %d calls %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if strings.Join(got[i], "\x00") != strings.Join(tt.want[i], "\x00") {
					t.Errorf("call %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPollScreenFailures(t *testing.T) {
	var calls int
	health := &mockRegistryHealth{}

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
		id:         "surface:1",
		surfaceID:  "surface:1",
		runner:     mockRunner,
		invalidate: health,
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

	health.mu.Lock()
	defer health.mu.Unlock()
	if health.invalidateCalls < 1 {
		t.Fatalf("expected Invalidate to be called at least once, got %d", health.invalidateCalls)
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

func TestCmuxAdapter_Contract(t *testing.T) {
	factory := func(t *testing.T) Adapter {
		health := &mockRegistryHealth{}
		var created []string
		return &cmuxAdapter{
			runner: &mockCmuxRunner{
				runFunc: func(ctx context.Context, _ CommandOptions, args ...string) ([]byte, error) {
					if len(args) == 0 {
						return nil, nil
					}
					switch args[0] {
					case "new-surface":
						const newID = "surface:99"
						created = append(created, newID)
						return []byte("Created new surface: " + newID + " (workspace: workspace:1)"), nil
					case "close-surface":
						for i, id := range created {
							if len(args) >= 3 && args[2] == id {
								created = append(created[:i], created[i+1:]...)
								break
							}
						}
						return nil, nil
					case "tree":
						if ctx.Err() != nil {
							return nil, ctx.Err()
						}
						base := `window window:1 [current]
├── workspace workspace:1 "DevRemote"
│   └── pane pane:1
│       └── surface surface:1 [terminal] "dev" tty=ttys000
├── workspace workspace:2 "Build"
│   └── pane pane:2
│       └── surface surface:2 [terminal] "build" tty=ttys001`
						for _, id := range created {
							base += "\n│       └── surface " + id + ` [terminal] "contract" tty=ttys099`
						}
						return []byte(base), nil
					case "read-screen":
						for _, a := range args {
							if a == "--scrollback" {
								return []byte("scrollback history"), nil
							}
						}
						return []byte("screen content"), nil
					case "send", "send-key":
						return nil, nil
					case "top":
						return []byte("tag\ttag:agent1\tworkspace:1\tclaude\nprocess\t12345\tsurface:1\tbash"), nil
					}
					return nil, nil
				},
			},
			invalidate: health,
		}
	}

	cfg := &ContractConfig{
		ExpectScreenReader:      true,
		ExpectHistoryReader:     true,
		ExpectStreamOpener:      true,
		ExpectProcessProvider:   true,
		ExpectSessionCreator:    true,
		ExpectSessionTerminator: true,
		ExpectProcessSnapshot:   true,
	}

	RunAdapterContract(t, "cmux", factory)
	RunScreenHistoryContract(t, cfg, factory)
	RunProcessInfoContract(t, cfg, factory)
	RunProcessSnapshotContract(t, cfg, factory)
	RunCreateDiscoverTerminateContract(t, cfg, factory)
	RunLiveStreamContract(t, cfg, factory)
}

func TestNewCmuxAdapterRejectsNilHealth(t *testing.T) {
	if _, err := NewCmuxAdapter(nil); err == nil {
		t.Fatal("expected nil RegistryHealth to be rejected")
	}
}

func TestNewCmuxAdapterStoresHealth(t *testing.T) {
	health := &mockRegistryHealth{}
	adapter, err := NewCmuxAdapter(health)
	if err != nil {
		t.Fatalf("NewCmuxAdapter failed: %v", err)
	}
	cmux, ok := adapter.(*cmuxAdapter)
	if !ok {
		t.Fatalf("adapter type = %T, want *cmuxAdapter", adapter)
	}
	if cmux.invalidate != health {
		t.Fatal("adapter did not retain the supplied RegistryHealth")
	}
}

func (m *mockRegistryHealth) InvalidateAdapter(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invalidateCalls++
}

type mockRegistryHealth struct {
	mu              sync.Mutex
	calls           int
	invalidateCalls int
	name            string
	force           bool
	refreshErr      error
}

func (m *mockRegistryHealth) Refresh(_ context.Context, name string, force bool) (AdapterSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.name = name
	m.force = force
	return AdapterSnapshot{}, m.refreshErr
}
