package wrap

import (
	"bytes"
	"encoding/json"
	"regexp"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// IPCState is written to a temp file so the daemon can discover
// this PTY and inject approved responses.
type IPCState struct {
	PID  int    `json:"pid"`
	PTY  string `json:"pty,omitempty"`
	Port int    `json:"port,omitempty"`
}

// ReadIPC reads the IPC state file written by the wrap process.
func ReadIPC(dir string) (IPCState, error) {
	path := dir + "/devremote_wrap.json"
	if runtime.GOOS == "windows" {
		path = dir + "\\devremote_wrap.json"
	}
	var state IPCState
	f, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer f.Close()
	err = json.NewDecoder(f).Decode(&state)
	return state, err
}

// WriteIPC writes the IPC state file so the daemon can discover this wrapper.
func WriteIPC(dir string, state IPCState) (string, error) {
	path := dir + "/" + "devremote_wrap.json"
	if runtime.GOOS == "windows" {
		path = dir + "\\devremote_wrap.json"
	}
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("write ipc: %w", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(state); err != nil {
		return "", err
	}
	return path, nil
}

// Command runs the given command inside a PTY.
// Approval prompts detected in stdout are forwarded to daemon on port 9171.
func Command(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()

	tty, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	defer tty.Close()

	ptyPath, err := ptyName(tty, cmd.Process.Pid)
	if err != nil {
		ptyPath = "unknown"
	}

	// Start localhost HTTP server for stdin injection from the daemon.
	stdinPort, err := startStdinServer(tty)
	if err != nil {
		log.Printf("wrap: stdin server: %v", err)
		stdinPort = 0
	}

	ipcPath, err := WriteIPC(os.TempDir(), IPCState{
		PID:  cmd.Process.Pid,
		PTY:  ptyPath,
		Port: stdinPort,
	})
	if err != nil {
		log.Printf("wrap: ipc write failed: %v", err)
	} else {
		defer os.Remove(ipcPath)
	}

	resizeLoop(tty)

	// Relay stdin to PTY (keep as is).
	// Raw mode prevents terminal garbage (mouse events, escape codes).
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("make raw: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Stdin → PTY (preserves raw keys like Ctrl+C).
	go io.Copy(tty, io.TeeReader(os.Stdin, os.Stdout))

	// Relay PTY to stdout with approval detection.
	go scanAndRelay(tty, os.Stdout, 9171)

	return cmd.Wait()
}

// scanAndRelay reads from PTY, writes to stdout, and streams everything to the daemon
// for terminal mirroring on the phone. Approval prompts are detected and highlighted.
func scanAndRelay(src io.Reader, dst io.Writer, daemonPort int) {
	buf := make([]byte, 4096)
	var batch []byte
	var lastFlush = time.Now()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		text := cleanANSI(string(batch))
		if len(text) < 10 { // skip keystroke echoes
			batch = nil
			return
		}
		notifyRaw(daemonPort, text)
		// Also check for approval prompts — sent as alerts (not feed).
		if isApprovalPrompt([]byte(text)) {
			desc := lastQuestionLine([]byte(text))
			notifyApproval(daemonPort, desc)
		}
		batch = nil
	}

	for {
		n, err := src.Read(buf)
		if n > 0 {
			dst.Write(buf[:n])
			batch = append(batch, buf[:n]...)

			// Immediate check for approval prompts on every read.
			text := cleanANSI(string(buf[:n]))
			if len(text) > 20 && isApprovalPrompt([]byte(text)) {
				desc := lastQuestionLine([]byte(text))
				notifyApproval(daemonPort, desc)
			}

			// Flush every ~100ms for real-time feel.
			if time.Since(lastFlush) > 500*time.Millisecond || len(batch) > 4096 {
				flush()
				lastFlush = time.Now()
			}
		}
		if err != nil {
			flush() // flush remaining
			if err != io.EOF {
				log.Printf("wrap: read: %v", err)
			}
			return
		}
	}
}

var lastApprovalTime time.Time
var lastApprovalDesc string

// notifyApproval sends an approval alert to the daemon (cooldown: 5s).
func notifyApproval(daemonPort int, desc string) {
	if time.Since(lastApprovalTime) < 5*time.Second || desc == lastApprovalDesc {
		return
	}
	lastApprovalTime = time.Now()
	lastApprovalDesc = desc
	body, _ := json.Marshal(map[string]string{
		"type":        "approval_prompt",
		"description": desc,
		"toolName":    "Bash",
	})
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/approval", daemonPort),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// notifyRaw sends raw PTY output to the daemon's /pty endpoint.
func notifyRaw(daemonPort int, text string) {
	body, _ := json.Marshal(map[string]string{"text": text})
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/pty", daemonPort),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// isApprovalPrompt checks if recent PTY output looks like Claude is asking for approval.
func isApprovalPrompt(data []byte) bool {
	s := cleanANSI(string(data))
	// Pattern 1: Traditional (y/n) prompt.
	if strings.Contains(s, "(y/n)") || strings.Contains(s, "(y/N)") {
		return true
	}
	// Pattern 2: Numbered choice menu (Claude Code default).
	// e.g. "❯ 1. Yes\n   2. Yes, and don't ask again\n   3. No"
	if strings.Contains(s, "1. Yes") && strings.Contains(s, "No") {
		return true
	}
	// Pattern 3: Question mark at end (simple yes/no).
	if strings.HasSuffix(strings.TrimSpace(s), "?") {
		return true
	}
	// Pattern 4: "Do you want" or "proceed?" anywhere.
	lower := strings.ToLower(s)
	if strings.Contains(lower, "do you want") || strings.Contains(lower, "proceed?") {
		return true
	}
	return false
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\a]*\a|\x1b\]0;[^\a]*\a|\r`)

// cleanANSI strips ANSI escape sequences and terminal control chars.
func cleanANSI(s string) string {
	return strings.TrimSpace(ansiRe.ReplaceAllString(s, ""))
}

// lastQuestionLine extracts the last meaningful line containing a question.
func lastQuestionLine(data []byte) string {
	s := cleanANSI(string(data))
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.Contains(line, "?") || strings.Contains(line, "(y/n)") ||
			strings.Contains(line, "1. Yes") || strings.Contains(line, "Do you want") {
			return line
		}
	}
	return "Claude 승인이 필요합니다"
}

// notifyDaemon POSTs an approval alert to the daemon's HTTP endpoint.
func notifyDaemon(daemonPort int, desc string) {
	if daemonPort == 0 {
		return
	}
	body, _ := json.Marshal(map[string]string{
		"type":        "approval_prompt",
		"description": desc,
		"toolName":    "Bash",
	})
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/approval", daemonPort),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		log.Printf("wrap: notify daemon: %v", err)
		return
	}
	resp.Body.Close()
}

// startStdinServer opens a localhost HTTP server on a random port.
// POST /stdin with {"text":"y\n"} writes to the PTY.
func startStdinServer(tty *os.File) (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/stdin", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(400)
			return
		}
		if req.Text != "" {
			tty.Write([]byte(req.Text))
		}
		w.WriteHeader(200)
	})

	port := listener.Addr().(*net.TCPAddr).Port
	go http.Serve(listener, mux)
	return port, nil
}

func ptyName(master *os.File, pid int) (string, error) {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("conpty-%d", pid), nil
	}
	return os.Readlink(fmt.Sprintf("/proc/self/fd/%d", master.Fd()))
}
