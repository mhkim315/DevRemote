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
	session := r.URL.Query().Get("session")
	
	// 1. Use the selected multiplexer (tmux, cumx, etc.) to ensure session persistence
	cmd := DefaultMux.AttachCmd(session)
	tty, err := pty.Start(cmd)
	if err != nil {
		// Fallback to bash if multiplexer is not available
		cmd = exec.Command("bash")
		tty, err = pty.Start(cmd)
		if err != nil {
			http.Error(w, "pty failed", 500)
			return
		}
	}
	defer tty.Close()
	// Do NOT kill cmd.Process here, so tmux session stays alive in background!
	log.Printf("WS connected: %s", r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				data := buf[:n]
				conn.WriteMessage(websocket.TextMessage, data)
				if OnApproval != nil && isApprovalPrompt(data) {
					go OnApproval("Agent Claude requires your approval!")
				}
			}
			if err != nil {
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
	io.WriteString(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/><script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.min.js"></script><style>*{margin:0;padding:0}html,body{width:100%;height:100%;background:#000}#t{width:100%;height:100%}</style></head><body><div id="t"></div><script>var term=new Terminal({fontSize:12,fontFamily:'Menlo,Monaco,"Courier New",monospace',theme:{background:"#000",foreground:"#ccc"}});term.open(document.getElementById("t"));var protocol=location.protocol==='https:'?'wss://':'ws://';var ws=window.ws=new WebSocket(protocol+location.host+"/term/ws"+location.search);var raw='';ws.binaryType='arraybuffer';ws.onmessage=function(e){var t=typeof e.data==='string'?e.data:new TextDecoder().decode(e.data);raw+=t;term.write(t)};term.onData(function(d){ws.send(d)});setTimeout(function(){term.focus()},500);setInterval(function(){var s=raw.slice(-2048).replace(/\x1b\[[0-9;]*[a-zA-Z]/g,'').replace(/[\x00-\x08\x0b\x0c\x0e-\x1f]/g,'');fetch("/debug/dump",{method:"POST",body:s}).catch(function(){});fetch("/debug/cmd").then(function(r){return r.text()}).then(function(t){if(t)ws.send(t+"\n")}).catch(function(){})},2000);</script></body></html>`)
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

func HandleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := DefaultMux.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	
	// Create JSON array
	jsonBytes := []byte("[")
	for i, s := range sessions {
		if i > 0 {
			jsonBytes = append(jsonBytes, ',')
		}
		jsonBytes = append(jsonBytes, []byte(`"`+s+`"`)...)
	}
	jsonBytes = append(jsonBytes, ']')
	w.Write(jsonBytes)
}

func HandleDump(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if len(body) > 0 {
		log.Printf("PHONE: %s", string(body))
	}
}
