package term

import (
	"bufio"
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
	// First line is the command, e.g. "cmd:claude"
	line, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("IPC read error: %v", err)
		return
	}

	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "cmd:") {
		log.Printf("IPC invalid command format: %s", line)
		return
	}

	cmdStr := strings.TrimPrefix(line, "cmd:")

	// Spawn a new native multiplexer session
	sessionID := cmdStr // Simple ID for now
	s, err := mux.NewSession(sessionID, "sh", "-c", cmdStr)
	if err != nil {
		log.Println("failed to spawn session:", err)
		return
	}

	// Stream PTY stdout to IPC connection
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := s.PTY.Read(buf)
			if err != nil {
				break
			}
			conn.Write(buf[:n])
		}
	}()

	// Stream IPC connection to PTY stdin
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			break
		}
		s.Write(buf[:n])
	}
}
