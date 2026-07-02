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

// OnApproval is called when Claude asks for user approval.
var OnApproval func(string)

func HandleWS(w http.ResponseWriter, r *http.Request) {
	// 1. Use tmux to ensure session persistence across WebSocket reconnects
	cmd := exec.Command("tmux", "new-session", "-A", "-s", "devremote")
	tty, err := pty.Start(cmd)
	if err != nil {
		// Fallback to bash if tmux is not installed
		cmd = exec.Command("bash")
		tty, err = pty.Start(cmd)
		if err != nil {
			http.Error(w, "pty failed", 500)
			return
		}
	}
	defer tty.Close()
	// Do NOT kill cmd.Process here, so tmux session stays alive in background!

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	defer conn.Close()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				data := buf[:n]
				conn.WriteMessage(websocket.TextMessage, data)
				
				// 2. Approval detection: Send push notification via callback
				if OnApproval != nil && isApprovalPrompt(data) {
					// We launch it in a goroutine so it doesn't block PTY reading
					go OnApproval("Agent Claude requires your approval!")
				}
			}
			if err != nil {
				log.Printf("pty read err or closed: %v", err)
				return
			}
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		tty.Write(msg)
	}
}

func isApprovalPrompt(data []byte) bool {
	return bytes.Contains(data, []byte("Do you want")) ||
		bytes.Contains(data, []byte("proceed?")) ||
		bytes.Contains(data, []byte("(y/n)")) ||
		bytes.Contains(data, []byte("(y/N)")) ||
		(bytes.Contains(data, []byte("1. Yes")) && bytes.Contains(data, []byte("No")))
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
