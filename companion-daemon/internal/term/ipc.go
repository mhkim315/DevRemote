package term

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	"devremote/companion-daemon/internal/mux"
)

// IPCServer owns a Unix domain socket listener and its accept goroutine.
// Close stops the listener and Wait blocks until the goroutine has returned.
type IPCServer struct {
	listener  net.Listener
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
	reg       *mux.Registry
	events    EventStore
	links     LinkStore
	telemetry *TelemetryService
}

// StartIPCServer creates a Unix Domain Socket server for local 'pokit run' commands.
// The caller owns the returned IPCServer and must call Close + Wait to clean up.
func StartIPCServer(socketPath string, reg *mux.Registry, events EventStore, links LinkStore, telemetry *TelemetryService) (*IPCServer, error) {
	// Clean up old socket if it exists
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			return nil, err
		}
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	log.Printf("IPC Server listening on %s", socketPath)

	srv := &IPCServer{
		listener:  listener,
		done:      make(chan struct{}),
		reg:       reg,
		events:    events,
		links:     links,
		telemetry: telemetry,
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
		go handleIPCConnection(conn, s.reg, s.events, s.links, s.telemetry)
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

func handleIPCConnection(conn net.Conn, reg *mux.Registry, events EventStore, links LinkStore, telemetry *TelemetryService) {
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
			Version           int    `json:"version"`
			Operation         string `json:"operation"`
			SessionID         string `json:"sessionId"`
			Provider          string `json:"provider"`
			ExternalSessionID string `json:"externalSessionId"`
		}
		if err := json.NewDecoder(reader).Decode(&req); err != nil {
			conn.Write([]byte(fmt.Sprintf("error decoding json: %v\n", err)))
			return
		}

		if req.Operation == "link" {
			link := SessionLink{
				SessionID:         req.SessionID,
				Provider:          req.Provider,
				ExternalSessionID: req.ExternalSessionID,
			}
			if err := LinkSession(link, reg, links, events); err != nil {
				conn.Write([]byte(fmt.Sprintf("error linking: %v\n", err)))
			} else {
				if telemetry != nil {
					telemetry.Clear(mux.MigrateLegacyID(link.SessionID))
				}
				conn.Write([]byte("linked\n"))
			}
		} else if req.Operation == "unlink" {
			if err := UnlinkSession(req.SessionID, reg, links, events); err != nil {
				conn.Write([]byte(fmt.Sprintf("error unlinking: %v\n", err)))
			} else {
				if telemetry != nil {
					telemetry.Clear(mux.MigrateLegacyID(req.SessionID))
				}
				conn.Write([]byte("unlinked\n"))
			}
		} else if req.Operation == "links" {
			allLinks := GetAllLinks(links)
			json.NewEncoder(conn).Encode(allLinks)
		} else {
			conn.Write([]byte("unknown operation\n"))
		}
		return
	}

	// Fallback to legacy plain-text protocol
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
	if reader.Buffered() > 0 {
		bufferedData, _ := reader.Peek(reader.Buffered())
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
		if writer, ok := s.(mux.InputWriter); ok {
			writer.WriteInput(context.Background(), buf[:n])
		} else {
			stream.Write(buf[:n])
		}
	}
}
