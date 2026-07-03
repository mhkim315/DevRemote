package term

import (
	"bytes"
	"encoding/json"
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
				if OnApproval != nil {
					if matched, promptStr := isApprovalPrompt(data); matched {
						go OnApproval(promptStr)
					}
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

func isApprovalPrompt(data []byte) (bool, string) {
	if !bytes.Contains(data, []byte("Do you want")) &&
		!bytes.Contains(data, []byte("proceed?")) &&
		!bytes.Contains(data, []byte("(y/n)")) &&
		!bytes.Contains(data, []byte("(y/N)")) &&
		!(bytes.Contains(data, []byte("1. Yes")) && bytes.Contains(data, []byte("No"))) {
		return false, ""
	}
	
	// Clean ANSI for push notification
	cleanLine := ""
	inEsc := false
	for _, b := range data {
		if b == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') {
				inEsc = false
			}
			continue
		}
		// Allow printable ascii and basic whitespace
		if b >= 32 && b <= 126 || b == '\n' || b == '\t' {
			cleanLine += string(b)
		}
	}
	
	if len(cleanLine) > 150 {
		cleanLine = "..." + cleanLine[len(cleanLine)-150:]
	}
	return true, "Agent: " + cleanLine
}

func HandleHTML(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/><script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.min.js"></script><style>*{margin:0;padding:0}html,body{width:100%;height:100%;background:#000}#t{width:100%;height:100%}#status{position:fixed;top:4px;right:8px;color:#888;font:12px monospace;z-index:9;padding:2px 8px;border-radius:4px;background:rgba(0,0,0,0.7)}</style></head><body><div id="t"></div><div id="status">connecting</div><script>function connect(){var protocol=location.protocol==='https:'?'wss://':'ws://';var s=document.getElementById('status');var ws=new WebSocket(protocol+location.host+"/term/ws"+location.search);window.ws=ws;ws.binaryType='arraybuffer';s.textContent='connecting';s.style.color='#e3b341';var raw='';ws.onopen=function(){s.textContent='live';s.style.color='#238636'};ws.onmessage=function(e){var t=typeof e.data==='string'?e.data:new TextDecoder().decode(e.data);raw+=t;term.write(t)};ws.onclose=function(){s.textContent='reconnecting';s.style.color='#f85149';setTimeout(connect,1000)};ws.onerror=function(){s.textContent='error';s.style.color='#f85149'};term.onData(function(d){try{ws.send(d)}catch(e){}});setInterval(function(){var t=raw.slice(-2048).replace(/\x1b\[[0-9;?]*[a-zA-Z]/g,'').replace(/[\x00-\x08\x0b\x0c\x0e-\x1f]/g,'');fetch("/debug/dump"+location.search,{method:"POST",body:t}).catch(function(){});fetch("/debug/cmd"+location.search).then(function(r){return r.text()}).then(function(d){if(d&&ws.readyState===1)ws.send(d+"\n")}).catch(function(){})},2000)}var term=new Terminal({fontSize:12,fontFamily:'Menlo,Monaco,"Courier New",monospace',theme:{background:"#000",foreground:"#ccc"}});term.open(document.getElementById("t"));setTimeout(function(){term.focus()},500);connect();</script></body></html>`)
}

var (
	pendingCmds = make(map[string]string)
	cmdMu       sync.Mutex
)

func HandleCmd(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}
	if r.Method == "POST" {
		body, _ := io.ReadAll(r.Body)
		cmdMu.Lock()
		pendingCmds[session] = string(body)
		log.Printf("CMD POST [%s]: %q", session, pendingCmds[session])
		cmdMu.Unlock()
		w.WriteHeader(200)
		return
	}
	cmdMu.Lock()
	cmd := pendingCmds[session]
	if cmd != "" {
		pendingCmds[session] = ""
	}
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
	if sessions == nil {
		sessions = []string{}
	}
	
	jsonBytes, _ := json.Marshal(sessions)
	w.Write(jsonBytes)
}

func HandleDump(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}
	body, _ := io.ReadAll(r.Body)
	if len(body) > 0 {
		log.Printf("PHONE [%s]: %s", session, string(body))
	}
}
