package term

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"devremote/companion-daemon/internal/mux"
)

// StartIPCServer starts a Unix Domain Socket server to listen for local 'pokit run' commands.
func StartIPCServer(socketPath string) error {
	// Clean up old socket if it exists
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			return err
		}
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}

	log.Printf("IPC Server listening on %s", socketPath)

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("IPC accept error: %v", err)
				continue
			}
			go handleIPCConnection(conn)
		}
	}()

	return nil
}

func handleIPCConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	
	cmdStr := ""
	termEnv := "xterm-256color"
	var initialW, initialH int

	// Parse headers until empty line (EOH)
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
	s, err := mux.FindSession(sessionID)
	if err != nil {
		s, err = mux.NewSession(sessionID, termEnv, "bash", "-c", cmdStr)
		if err != nil {
			log.Println("failed to spawn session:", err)
			return
		}
	}
	defer s.Close()

	// Set initial PTY size if provided
	if initialW > 0 && initialH > 0 {
		if szErr := s.Resize(initialH, initialW); szErr != nil {
			log.Printf("IPC resize err: %v", szErr)
		}
	}

	// Stream PTY stdout to IPC connection securely
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := s.Read(buf)
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
		s.Write(bufferedData)
		reader.Discard(reader.Buffered())
	}
	
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		s.Write(buf[:n])
	}
}
