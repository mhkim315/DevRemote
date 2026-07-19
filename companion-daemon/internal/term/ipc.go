package term

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// IPCServer owns a Unix domain socket listener and its accept goroutine.
// Close stops the listener and Wait blocks until the goroutine has returned.
type IPCServer struct {
	listener      net.Listener
	done          chan struct{}
	closeOnce     sync.Once
	closeErr      error
	reg           *mux.Registry
	telemetry     *TelemetryService
	lifecycle     *LifecycleService
	managed       *ManagedCodexService  // SP0: nil unless EnableManagedCodex
	managedClaude *ManagedClaudeService // C1D: nil unless EnableManagedClaude
}

// StartIPCServer creates a Unix Domain Socket server for local 'pokit run' commands.
// The caller owns the returned IPCServer and must call Close + Wait to clean up.
func StartIPCServer(socketPath string, reg *mux.Registry, telemetry *TelemetryService, lifecycle *LifecycleService, managed *ManagedCodexService, managedClaude *ManagedClaudeService) (*IPCServer, error) {
	// If a socket file already exists, only remove it when it is stale. If a
	// live daemon is still listening on it, refuse: otherwise a duplicate
	// daemon start would delete the running daemon's socket and then fail on
	// the TCP bind, leaving the running daemon alive with no socket file.
	if _, err := os.Stat(socketPath); err == nil {
		if conn, derr := net.DialTimeout("unix", socketPath, 500*time.Millisecond); derr == nil {
			conn.Close()
			return nil, fmt.Errorf("IPC socket %s already in use by another daemon", socketPath)
		}
		// No listener answered — the socket is stale, safe to remove.
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("remove stale socket %s: %w", socketPath, err)
		}
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	// Enforce user-only access on the socket.
	if err := os.Chmod(socketPath, 0600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("chmod %s: %w", socketPath, err)
	}

	log.Printf("IPC Server listening on %s", socketPath)

	srv := &IPCServer{
		listener:      listener,
		done:          make(chan struct{}),
		reg:           reg,
		telemetry:     telemetry,
		lifecycle:     lifecycle,
		managed:       managed,
		managedClaude: managedClaude,
	}

	go srv.serve()
	return srv, nil
}

// serve runs the accept loop. It returns when the listener is closed.
func (s *IPCServer) serve() {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return // normal shutdown
			}
			log.Printf("IPC accept error: %v", err)
			return // unexpected error, stop serving
		}
		go handleIPCConnection(conn, s.reg, s.telemetry, s.lifecycle, s.managed, s.managedClaude)
	}
}

// Close stops the listener. It is idempotent.
func (s *IPCServer) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.listener.Close()
	})
	return s.closeErr
}

// Wait blocks until the accept goroutine returns or ctx is cancelled.
// Returns an error only if the deadline expires.
func (s *IPCServer) Wait(ctx context.Context) error {
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func handleIPCConnection(conn net.Conn, reg *mux.Registry, telemetry *TelemetryService, lifecycle *LifecycleService, managed *ManagedCodexService, managedClaude *ManagedClaudeService) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	cmdStr := ""
	termEnv := "xterm-256color"
	var initialW, initialH int

	// Check if the connection starts with JSON (new protocol)
	firstByte, err := reader.Peek(1)
	if err != nil {
		log.Printf("IPC read error: %v", err)
		return
	}
	if firstByte[0] == '{' {
		// It's a JSON request
		var req struct {
			Version   int    `json:"version"`
			Operation string `json:"operation"`
			SessionID string `json:"sessionId"`
			// create (privileged local launch — 0600 socket only)
			Command    json.RawMessage `json:"command"`
			CWD        string          `json:"cwd"`
			Name       string          `json:"name"`
			ProfileID  string          `json:"profileId"`
			Executable string          `json:"executable"`
			Args       []string        `json:"args"`
			Detach     bool            `json:"detach"` // SP0: structured detached launch
			Cursor     uint64          `json:"cursor"` // SP0.5: managed-attach event cursor
			// pair-start / pair-approve / pair-reject (M2.5-2)
			Duration       int    `json:"duration"`
			PhoneSignature []byte `json:"phoneSignature,omitempty"`
			// device admin (M2.5-5, local 0600 socket only)
			DeviceID string `json:"deviceId"`
			Limit    int    `json:"limit"`
		}
		dec := json.NewDecoder(reader)
		if err := dec.Decode(&req); err != nil {
			conn.Write([]byte(fmt.Sprintf("error decoding json: %v\n", err)))
			return
		}

		if req.Operation == "create" {
			// Privileged local launch. The 0600 socket is reachable only by
			// local processes and is NOT forwarded through the tunnel, so this
			// path may run the legacy command string or custom argv that HTTP
			// refuses.
			// SP0: conflicting launch inputs fail closed BEFORE any dispatch —
			// a request may select exactly one of profile, executable, command.
			if err := validateLaunchInputExclusivity(req.ProfileID, req.Executable, req.Command); err != nil {
				json.NewEncoder(conn).Encode(map[string]string{"error": err.Error()})
				return
			}
			// SP0/SP0.5: the recognized Codex profile is a MANAGED-ONLY
			// request — detached keeps the SP0 certification-turn launch,
			// non-detached creates a prompt-driven interactive session. With
			// the feature disabled it fails closed: the same structured
			// request must never silently fall back to the legacy
			// controlled_pty authority model (no child, no session, no
			// launcher). The exact legacy command-string invocation of codex
			// is likewise removed rather than shell-executed.
			if req.ProfileID == "codex" {
				if managed == nil {
					json.NewEncoder(conn).Encode(map[string]string{"error": "managed codex runtime unavailable: daemon started without --enable-managed-codex"})
					return
				}
				var id string
				var merr error
				if req.Detach {
					id, merr = managed.CreateDetached(req.CWD)
				} else {
					id, merr = managed.CreateAttached(req.CWD)
				}
				if merr != nil {
					json.NewEncoder(conn).Encode(map[string]string{"error": merr.Error()})
				} else {
					json.NewEncoder(conn).Encode(map[string]string{"id": id, "state": string(LifecycleRunning)})
				}
				return
			}
			// C1D: the recognized Claude profile is a MANAGED-ONLY request.
			// With the feature disabled it fails closed: the same structured
			// request must never silently fall back to controlled_pty.
			if req.ProfileID == "claude" {
				if managedClaude == nil {
					json.NewEncoder(conn).Encode(map[string]string{"error": "managed claude runtime unavailable: daemon started without --enable-managed-claude"})
					return
				}
				id, merr := managedClaude.CreateDetached(req.CWD)
				if merr != nil {
					json.NewEncoder(conn).Encode(map[string]string{"error": merr.Error()})
				} else {
					json.NewEncoder(conn).Encode(map[string]string{"id": id, "state": string(LifecycleRunning)})
				}
				return
			}
			if legacyCodexCommand(req.Command) {
				json.NewEncoder(conn).Encode(map[string]string{"error": "the codex command string is no longer executed via shell: use the structured codex profile (managed runtime)"})
				return
			}
			var ownedPTY *OwnedPTYRuntime
			if lifecycle != nil {
				ownedPTY = lifecycle.OwnedPTY()
			}
			id, state, cerr := createLocalControlled(context.Background(), ownedPTY, localCreateSpec{
				ProfileID:  req.ProfileID,
				Name:       req.Name,
				CWD:        req.CWD,
				Executable: req.Executable,
				Args:       req.Args,
				Command:    req.Command,
			})
			if cerr != nil {
				json.NewEncoder(conn).Encode(map[string]string{"error": cerr.Error()})
			} else {
				json.NewEncoder(conn).Encode(map[string]string{"id": id, "state": string(state)})
			}
		} else if req.Operation == "managed-attach" {
			// SP0.5: bounded structured local attach to a managed session.
			// The JSON decoder may have buffered bytes past the request line
			// (kernel socket writes coalesce) — recover them, or early prompt
			// lines would be silently swallowed.
			attachReader := bufio.NewReader(io.MultiReader(dec.Buffered(), reader))
			handleManagedAttach(conn, attachReader, managed, req.SessionID, req.Cursor)
			return
		} else if req.Operation == "pair-start" {
			handlePairOp(conn, req.Operation, req.Duration, nil)
			return
		} else if req.Operation == "pair-approve" {
			handlePairOp(conn, req.Operation, 0, req.PhoneSignature)
			return
		} else if req.Operation == "pair-reject" {
			handlePairOp(conn, req.Operation, 0, nil)
			return
		} else if req.Operation == "devices-list" {
			handleDevicesList(conn)
			return
		} else if req.Operation == "devices-revoke" {
			handleDevicesRevoke(conn, req.DeviceID)
			return
		} else if req.Operation == "audit-list" {
			handleAuditList(conn, req.Limit)
			return
		} else {
			conn.Write([]byte("unknown operation\n"))
		}
		return
	}

	// E10b: subscriber protocol — first line may be "sub:<sessionID>".
	firstLine, err := reader.ReadString('\n')
	if err == nil {
		firstLine = strings.TrimSpace(firstLine)
		if strings.HasPrefix(firstLine, "sub:") {
			// Format: "sub:<sessionID> [<cols> <rows>]". The session ID has no
			// spaces, so the optional local terminal size follows as fields.
			fields := strings.Fields(strings.TrimPrefix(firstLine, "sub:"))
			subID := ""
			var cols, rows int
			if len(fields) > 0 {
				subID = fields[0]
			}
			if len(fields) >= 3 {
				cols, _ = strconv.Atoi(fields[1])
				rows, _ = strconv.Atoi(fields[2])
			}
			handleIPCSubscriber(conn, subID, cols, rows)
			return
		}
		// Not sub: — process as first legacy header line.
		if firstLine != "" {
			if strings.HasPrefix(firstLine, "cmd:") {
				cmdStr = strings.TrimPrefix(firstLine, "cmd:")
			} else if strings.HasPrefix(firstLine, "term:") {
				termEnv = strings.TrimPrefix(firstLine, "term:")
			} else if strings.HasPrefix(firstLine, "size:") {
				fmt.Sscanf(firstLine, "size:%dx%d", &initialW, &initialH)
			}
		}
	}

	// Fallback to legacy plain-text protocol (remaining headers)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("IPC read error: %v", err)
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			break // End of Headers
		}

		if strings.HasPrefix(line, "cmd:") {
			cmdStr = strings.TrimPrefix(line, "cmd:")
		} else if strings.HasPrefix(line, "term:") {
			termEnv = strings.TrimPrefix(line, "term:")
		} else if strings.HasPrefix(line, "size:") {
			fmt.Sscanf(line, "size:%dx%d", &initialW, &initialH)
		}
	}

	if cmdStr == "" {
		log.Printf("IPC missing cmd header")
		return
	}

	// Spawn a new native multiplexer session, or use existing one
	sessionID := cmdStr // Simple ID for now
	s, err := reg.FindSession(context.Background(), sessionID)
	if err != nil {
		s, err = mux.NewSession(sessionID, termEnv, "bash", "-c", cmdStr)
		if err != nil {
			log.Println("failed to spawn session:", err)
			return
		}
	}

	var stream mux.TerminalStream
	if opener, ok := s.(mux.StreamOpener); ok {
		stream, err = opener.OpenStream(context.Background())
		if err != nil {
			log.Println("failed to open IPC stream:", err)
			return
		}
		defer stream.Close()
	} else {
		log.Println("session does not support opening streams")
		return
	}

	// Set initial PTY size if provided
	if initialW > 0 && initialH > 0 {
		if szErr := stream.Resize(initialH, initialW); szErr != nil {
			log.Printf("IPC resize err: %v", szErr)
		}
	}

	// Stream PTY stdout to IPC connection securely
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stream.Read(buf)
			if err != nil {
				break
			}
			if _, wErr := conn.Write(buf[:n]); wErr != nil {
				break
			}
		}
	}()

	// Stream IPC connection to PTY stdin (Raw byte copy)
	ts := GetTranscriptService()
	canonicalID := fmt.Sprintf("%s:%s", s.AdapterName(), s.ID())
	if reader.Buffered() > 0 {
		bufferedData, _ := reader.Peek(reader.Buffered())
		if ts != nil {
			ts.BeginInput(canonicalID, time.Now())
		}
		if writer, ok := s.(mux.InputWriter); ok {
			writer.WriteInput(context.Background(), bufferedData)
		} else {
			stream.Write(bufferedData)
		}
		reader.Discard(reader.Buffered())
	}

	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		if ts != nil {
			ts.BeginInput(canonicalID, time.Now())
		}
		if writer, ok := s.(mux.InputWriter); ok {
			writer.WriteInput(context.Background(), buf[:n])
		} else {
			stream.Write(buf[:n])
		}
	}
}

// handleIPCSubscriber bridges a local terminal to an existing recorder.
// The terminal is a subscriber — reads from recorder broadcast, writes
// via WriteInput. No second PTY reader is created.
func handleIPCSubscriber(conn net.Conn, sessionID string, cols, rows int) {
	rec := GetRecorder(sessionID)
	if rec == nil {
		conn.Write([]byte("session not found or recorder not started\n"))
		return
	}

	// Resize the PTY to the local terminal size before streaming so TUIs
	// (claude/codex) render at the attaching terminal's width, not the
	// 80x24 spawn default.
	if cols > 0 && rows > 0 {
		if err := rec.Resize(rows, cols); err != nil {
			log.Printf("IPC subscriber resize err session=%s: %v", sessionID, err)
		}
	}

	// E10b: atomic subscribe+bootstrap — no gap, no duplicate.
	bootstrap, subCh := rec.SubscribeWithBootstrap()
	if len(bootstrap) > 0 {
		conn.Write(bootstrap)
	}
	defer rec.Unsubscribe(subCh)

	// Recorder broadcast → local stdout. When subCh closes (session ended)
	// or a write fails, close conn so the read loop below unblocks and the
	// local client sees EOF — otherwise both sides deadlock on exit.
	go func() {
		for data := range subCh {
			if _, err := conn.Write(data); err != nil {
				break
			}
		}
		conn.Close()
	}()

	// Local stdin → recorder WriteInput.
	// T3: echo privacy — suppress byte-stream projection during input.
	ts := GetTranscriptService()
	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			if ts != nil {
				ts.BeginInput(sessionID, time.Now())
			}
			rec.WriteInput(buf[:n])
		}
	}
}
