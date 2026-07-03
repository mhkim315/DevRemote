package term

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// OnApproval is called when Claude asks for user approval.
var OnApproval func(string)

// sessionConns tracks the active WebSocket per session to prevent multiple PTYs.
var (
	sessionConns   = map[string]*websocket.Conn{}
	sessionConnsMu sync.Mutex
)

func HandleWS(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}

	// Kick old connection for this session to prevent PTY pile-up
	sessionConnsMu.Lock()
	if old, ok := sessionConns[session]; ok {
		old.Close()
	}
	sessionConnsMu.Unlock()

	// Use the selected multiplexer (tmux, cumx, etc.)
	cmd := DefaultMux.AttachCmd(session)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	tty, err := pty.Start(cmd)
	if err != nil {
		cmd = exec.Command("bash")
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")
		tty, err = pty.Start(cmd)
		if err != nil {
			http.Error(w, "pty failed", 500)
			return
		}
	}
	defer tty.Close()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Register this connection
	sessionConnsMu.Lock()
	sessionConns[session] = conn
	sessionConnsMu.Unlock()
	defer func() {
		sessionConnsMu.Lock()
		if sessionConns[session] == conn {
			delete(sessionConns, session)
		}
		sessionConnsMu.Unlock()
	}()

	log.Printf("WS [%s]: %s", session, r.RemoteAddr)

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				data := buf[:n]
				conn.WriteMessage(websocket.BinaryMessage, data)
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
	io.WriteString(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/><script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.min.js"></script><style>*{margin:0;padding:0}html,body{width:100%;height:100%;background:#000}#t{width:100%;height:100%}#status{position:fixed;top:4px;right:8px;color:#888;font:12px monospace;z-index:9;padding:2px 8px;border-radius:4px;background:rgba(0,0,0,0.7)}</style></head><body><div id="t"></div><div id="status">connecting</div><script>var raw='',reconnecting=false,reconnectTimer=null,decoder=new TextDecoder("utf-8");function processData(buffer){var t=decoder.decode(buffer,{stream:true});raw+=t;term.write(new Uint8Array(buffer))}function connect(){if(reconnecting)return;var protocol=location.protocol==='https:'?'wss://':'ws://';var s=document.getElementById('status');if(window.ws)try{window.ws.onclose=null;window.ws.close()}catch(e){}var ws=new WebSocket(protocol+location.host+"/term/ws"+location.search);window.ws=ws;ws.binaryType='arraybuffer';s.textContent='connecting';s.style.color='#e3b341';ws.onopen=function(){s.textContent='live';s.style.color='#238636';reconnecting=false};ws.onmessage=function(e){if(typeof e.data==='string'){raw+=e.data;term.write(e.data)}else if(e.data instanceof ArrayBuffer){processData(e.data)}else if(e.data instanceof Blob){e.data.arrayBuffer().then(processData)}};ws.onclose=function(){if(!reconnecting){reconnecting=true;s.textContent='reconnecting';s.style.color='#f85149';reconnectTimer=setTimeout(function(){reconnecting=false;connect()},2000)}};ws.onerror=function(e){console.error('ws error', e)}}var term=new Terminal({fontSize:12,fontFamily:'Menlo,Monaco,"Courier New",monospace',theme:{background:"#000",foreground:"#ccc"}});term.open(document.getElementById("t"));term.onData(function(d){var w=window.ws;if(w&&w.readyState===1)try{w.send(d)}catch(e){}});setTimeout(function(){term.focus()},500);setInterval(function(){fetch("/debug/cmd"+location.search).then(function(r){return r.text()}).then(function(d){var w=window.ws;if(d&&w&&w.readyState===1)try{w.send(d+"\n")}catch(e){}}).catch(function(){})},2000);connect();</script></body></html>`)
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
