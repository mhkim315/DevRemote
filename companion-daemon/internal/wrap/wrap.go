package wrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

type IPCState struct {
	PID  int    `json:"pid"`
	PTY  string `json:"pty,omitempty"`
	Port int    `json:"port,omitempty"`
}

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
	return state, json.NewDecoder(f).Decode(&state)
}

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

func Command(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()

	tty, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	defer tty.Close()

	stdinPort, err := startStdinServer(tty)
	if err != nil {
		log.Printf("wrap: stdin server: %v", err)
		stdinPort = 0
	}

	ptyPath, _ := ptyName(tty, cmd.Process.Pid)
	ipcPath, err := WriteIPC(os.TempDir(), IPCState{
		PID: cmd.Process.Pid, PTY: ptyPath, Port: stdinPort,
	})
	if err != nil {
		log.Printf("wrap: ipc write failed: %v", err)
	} else {
		defer os.Remove(ipcPath)
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("make raw: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	resizeLoop(tty)

	// stdin → PTY (PTY naturally echoes to stdout).
	go io.Copy(tty, os.Stdin)

	// PTY → stdout + daemon streaming (raw, no ANSI cleaning).
	go streamPTY(tty, os.Stdout, 9171)

	return cmd.Wait()
}

func streamPTY(src io.Reader, dst io.Writer, daemonPort int) {
	buf := make([]byte, 4096)
	var batch []byte
	var lastFlush = time.Now()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		post(daemonPort, "pty", string(batch))
		batch = nil
	}

	for {
		n, err := src.Read(buf)
		if n > 0 {
			dst.Write(buf[:n])
			batch = append(batch, buf[:n]...)
			if time.Since(lastFlush) > 100*time.Millisecond || len(batch) > 4096 {
				flush()
				lastFlush = time.Now()
			}
		}
		if err != nil {
			flush()
			if err != io.EOF {
				log.Printf("wrap: read: %v", err)
			}
			return
		}
	}
}

func post(daemonPort int, endpoint, text string) {
	body, _ := json.Marshal(map[string]string{"text": text})
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/%s", daemonPort, endpoint),
		"application/json", bytes.NewReader(body),
	)
	if err != nil {
		return
	}
	resp.Body.Close()
}

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
		var req struct{ Text string `json:"text"` }
		if json.NewDecoder(r.Body).Decode(&req) == nil && req.Text != "" {
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
