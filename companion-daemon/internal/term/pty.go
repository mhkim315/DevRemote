package term

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func HandleWS(w http.ResponseWriter, r *http.Request) {
	cmd := exec.Command("bash")
	tty, err := pty.Start(cmd)
	if err != nil { http.Error(w, "pty failed", 500); return }
	defer tty.Close()
	defer cmd.Process.Kill()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	defer conn.Close()
	go func() {
		buf := make([]byte, 4096)
		for { n, _ := tty.Read(buf); if n > 0 { conn.WriteMessage(websocket.TextMessage, buf[:n]) } }
	}()
	for { _, msg, _ := conn.ReadMessage(); tty.Write(msg) }
}

func HandleHTML(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/><script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.min.js"></script><style>*{margin:0;padding:0}html,body{width:100%;height:100%;background:#000}#t{width:100%;height:100%}</style></head><body><div id="t"></div><script>var term=new Terminal({fontSize:12,fontFamily:'Menlo,Monaco,"Courier New",monospace',theme:{background:"#000",foreground:"#ccc"}});term.open(document.getElementById("t"));var protocol=location.protocol==='https:'?'wss://':'ws://';var ws=window.ws=new WebSocket(protocol+location.host+"/term/ws");ws.onmessage=function(e){term.write(e.data)};term.onData(function(d){ws.send(d)});setTimeout(function(){term.focus()},500);setInterval(function(){var s="";for(var i=0;i<term.rows;i++){var l=term.buffer.active.getLine(i);if(l)s+=l.translateToString(true)+"\n"}fetch("/debug/dump",{method:"POST",body:s}).catch(function(){});fetch("/debug/cmd").then(function(r){return r.text()}).then(function(t){if(t)ws.send(t+"\n")}).catch(function(){})},2000);</script></body></html>`)
}

var (
	pendingCmd string
	cmdMu      sync.Mutex
)

func HandleCmd(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		body, _ := io.ReadAll(r.Body)
		cmdMu.Lock()
		pendingCmd = string(body)
		log.Printf("CMD POST: %q", pendingCmd)
		cmdMu.Unlock()
		w.WriteHeader(200)
		return
	}
	cmdMu.Lock()
	cmd := pendingCmd
	pendingCmd = ""
	cmdMu.Unlock()
	w.Write([]byte(cmd))
}

func HandleDump(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	log.Printf("PHONE: %s", string(body))
}

// StartPTY starts a tmux shell in a PTY and reads its output.
func StartPTY(onMessage func([]byte), onPush func(string)) (io.Writer, error) {
	cmd := exec.Command("tmux", "new-session", "-A", "-s", "devremote")
	tty, err := pty.Start(cmd)
	if err != nil {
		// Fallback to bash if tmux is not installed
		cmd = exec.Command("bash")
		tty, err = pty.Start(cmd)
		if err != nil {
			return nil, err
		}
	}

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				data := buf[:n]
				
				// --- Phase 3: Claude Hook & Push Notification ---
				if bytes.Contains(data, []byte("[Approval Required]")) {
					log.Println("🚨 [PUSH NOTIFICATION] Agent Claude requires your approval!")
					if onPush != nil {
						onPush("Agent Claude requires your approval!")
					}
				}

				if onMessage != nil {
					onMessage(data)
				}
			}
			if err != nil {
				log.Printf("pty read err: %v", err)
				return
			}
		}
	}()

	return tty, nil
}
