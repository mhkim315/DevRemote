package term

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/mux"
	"github.com/gorilla/websocket"
)

func (h *Handlers) HandleSessionsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		h.HandleSessionsV2(w, r)
	} else {
		h.HandleSessionCRUD(w, r)
	}
}
func ExtractToken(r *http.Request) string {
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		return token[7:]
	}
	return r.URL.Query().Get("token")
}

func (h *Handlers) HandleSessionCRUD(w http.ResponseWriter, r *http.Request) {
	reg := h.Registry

	if r.Method == "POST" || r.Method == "PUT" {
		var req struct {
			ID          string `json:"id"`
			WorkspaceID string `json:"workspaceId"`
			Runner      string `json:"runner"`
			RunnerColor string `json:"runnerColor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		ref := mux.ParseSessionID(req.ID)
		if err := ref.Validate(); err != nil {
			http.Error(w, fmt.Sprintf("invalid session ID: %v", err), http.StatusBadRequest)
			return
		}

		if err := ref.Validate(); err != nil {
			http.Error(w, fmt.Sprintf("invalid session ID: %v", err), http.StatusBadRequest)
			return
		}

		adapterName := ref.Adapter
		if adapterName == "" {
			adapterName = "tmux" // Fallback
		}

		if r.Method == http.MethodPost {
			opts := mux.CreateOptions{Name: ref.LocalID, WorkspaceID: req.WorkspaceID}
			createdID, err := reg.CreateSession(r.Context(), adapterName, opts)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to create session: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			canonicalID := mux.SessionRef{Adapter: adapterName, LocalID: createdID}.Canonical()
			w.Write([]byte(fmt.Sprintf(`{"status":"ok","id":"%s"}`, canonicalID)))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
		return
	}

	if r.Method == "DELETE" {
		id := r.URL.Query().Get("id")
		if id != "" {
			ref := mux.ParseSessionID(id)
			if err := ref.Validate(); err != nil {
				http.Error(w, fmt.Sprintf("invalid session ID: %v", err), http.StatusBadRequest)
				return
			}

			if err := ref.Validate(); err != nil {
				http.Error(w, fmt.Sprintf("invalid session ID: %v", err), http.StatusBadRequest)
				return
			}

			adapterName := ref.Adapter
			if adapterName == "" {
				adapterName = "tmux"
			}

			if err := reg.TerminateSession(r.Context(), adapterName, ref.LocalID); err != nil {
				http.Error(w, fmt.Sprintf("failed to terminate session: %v", err), http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
		return
	}

	http.Error(w, "Method not allowed", 405)
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (h *Handlers) HandleWS(w http.ResponseWriter, r *http.Request) {
	reg := h.Registry

	// Extract JWT from Authorization header (preferred) or ?token= query param
	// Auth check is handled by middleware

	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}

	var s mux.Session
	var err error
	s, err = reg.FindSession(r.Context(), session)
	if err != nil {
		log.Printf("WS session not found err: %v", err)
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var stream mux.TerminalStream
	if opener, ok := s.(mux.StreamOpener); ok {
		stream, err = opener.OpenStream(r.Context())
		if err != nil {
			log.Printf("WS stream open err: %v", err)
			http.Error(w, "stream failed", 500)
			return
		}
		defer stream.Close()
	} else {
		http.Error(w, "Session does not support streaming", http.StatusNotImplemented)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS upgrade err: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("WS [%s]: %s connected", session, r.RemoteAddr)

	type wsOutbound struct {
		messageType int
		payload     []byte
	}

	outbound := make(chan wsOutbound, 32)
	fatalErr := make(chan error, 1)
	fatalRequested := make(chan struct{})
	shutdown := make(chan struct{})
	writerDone := make(chan struct{})
	var closeOnce sync.Once
	var shutdownOnce sync.Once

	// Helper to trigger a graceful close
	triggerClose := func(err error) {
		closeOnce.Do(func() {
			close(fatalRequested)
			fatalErr <- err
		})
	}
	stopWriter := func() {
		shutdownOnce.Do(func() {
			close(shutdown)
		})
	}

	// Single writer goroutine to prevent Gorilla WS panic
	go func() {
		defer close(writerDone)
		defer conn.Close()
		for {
			select {
			case msg := <-outbound:
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := conn.WriteMessage(msg.messageType, msg.payload); err != nil {
					return
				}
			case err := <-fatalErr:
				// Send 1011 close frame
				reason := "stream error"
				if err != nil {
					// Ensure valid UTF-8 and < 125 bytes
					errMsg := err.Error()
					if len(errMsg) > 100 {
						errMsg = errMsg[:100] + "..."
					}
					reason = "error: " + errMsg
				}
				conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(1011, reason))
				return
			case <-shutdown:
				return
			}
		}
	}()

	if sr, ok := s.(mux.ScreenReader); ok && s.AdapterName() == "tmux" {
		if initial, snapErr := sr.ReadScreen(r.Context()); snapErr == nil && len(initial) > 0 {
			payload := "\033[2J\033[H" + string(initial)
			payload = strings.ReplaceAll(payload, "\n", "\r\n")
			select {
			case outbound <- wsOutbound{messageType: websocket.BinaryMessage, payload: []byte(payload)}:
			case <-writerDone:
				return
			case <-r.Context().Done():
				return
			}
		}
	}

	if stream != nil {
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := stream.Read(buf)
				if err != nil {
					log.Printf("WS stream read err: %v", err)
					triggerClose(fmt.Errorf("stream failed"))
					break
				}
				// Copy buffer since we're passing it to channel
				payload := make([]byte, n)
				copy(payload, buf[:n])
				select {
				case outbound <- wsOutbound{messageType: websocket.BinaryMessage, payload: payload}:
				case <-writerDone:
					return
				case <-r.Context().Done():
					return
				}
			}
		}()
	}

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if writer, ok := s.(mux.InputWriter); ok {
			if inErr := writer.WriteInput(r.Context(), msg); inErr != nil {
				log.Printf("WS input write err: %v", inErr)
				triggerClose(fmt.Errorf("input failed"))
				break
			}
		} else if stream != nil {
			stream.Write(msg)
		}
	}

	// Ensure stream closes when client disconnects
	if stream != nil {
		stream.Close()
	}
	closeOnce.Do(func() {}) // prevent fatalErr channel block
	select {
	case <-fatalRequested:
		// The writer owns the 1011 close handshake.
	default:
		stopWriter()
	}
	<-writerDone
}

func (h *Handlers) HandleHTML(w http.ResponseWriter, r *http.Request) {
	// ... we will keep HandleHTML as is, though not heavily used
	// Auth check is handled by middleware

	io.WriteString(w, `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no">
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/>
<script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.min.js"></script>
<style>
*{margin:0;padding:0}
html,body{width:100%;height:100%;background:#000}
#t{width:100%;height:100%}
#status{position:fixed;top:4px;right:8px;color:#888;font:12px monospace;z-index:9;padding:2px 8px;border-radius:4px;background:rgba(0,0,0,0.7);display:none}
</style>
</head>
<body>
<div id="t"></div>
<div id="status"></div>
<script>
var raw='', reconnecting=false, opened=false, consecutiveFailures=0, stopped=false, cmdPoll=null;
var term=new Terminal({scrollback:50000,fontSize:12,fontFamily:'Menlo,Monaco,"Courier New",monospace',theme:{background:"#000",foreground:"#ccc"}});
term.open(document.getElementById("t"));

function setStatus(text, terminalText) {
  var s=document.getElementById('status');
  s.textContent=text;
  s.style.display='block';
  if (terminalText) term.writeln('\r\n' + terminalText);
}

function stopSession(text) {
  stopped=true;
  if (cmdPoll) clearInterval(cmdPoll);
  if (window.ws) try{window.ws.onclose=null;window.ws.close()}catch(e){}
  setStatus('session ended', text || 'Session ended');
}

function connect(){
  if(reconnecting||stopped)return;
  var protocol=location.protocol==='https:'?'wss://':'ws://';
  if(window.ws)try{window.ws.onclose=null;window.ws.close()}catch(e){}
  opened=false;
  var ws=new WebSocket(protocol+location.host+"/term/ws"+location.search);
  window.ws=ws;
  ws.binaryType='arraybuffer';
  ws.onopen=function(){
    opened=true;
    consecutiveFailures=0;
    reconnecting=false;
    document.getElementById('status').style.display='none';
    setTimeout(function(){fitTerminal()},500);
  };
  ws.onmessage=function(e){
    var t=typeof e.data==='string'?e.data:new TextDecoder().decode(e.data);
    raw+=t;
    term.write(t);
  };
  ws.onclose=function(e){
    if(stopped)return;
    if(!opened) consecutiveFailures++;
    if(e.code===1008||e.code===1011||consecutiveFailures>=3){
      stopSession('Session ended or unavailable');
      return;
    }
    if(!reconnecting){
      reconnecting=true;
      setStatus('reconnecting...');
      setTimeout(function(){reconnecting=false;connect()},2000);
    }
  };
  ws.onerror=function(){try{ws.close()}catch(e){}};
}

term.onData(function(d){
  var w=window.ws;
  if(w&&w.readyState===1)try{w.send(d)}catch(e){}
});

function fitTerminal(){
  var h=document.getElementById('t').clientHeight;
  var w=document.getElementById('t').clientWidth;
  var rows=Math.floor(h/17);
  var cols=Math.floor(w/7.8);
  if(rows>0&&cols>0){
    var p=new URLSearchParams(location.search);
    var sess=p.get('session');
    var tok=p.get('token');
    var hdrs={};
    if(tok)hdrs['Authorization']='Bearer '+tok;
    fetch('/term/size?session='+encodeURIComponent(sess)+'&rows='+rows+'&cols='+cols,{method:'POST',headers:hdrs}).catch(function(){});
  }
}

window.addEventListener('resize',function(){fitTerminal()});
cmdPoll=setInterval(function(){
  if(stopped)return;
  var p=new URLSearchParams(location.search);
  var sess=p.get('session');
  var tok=p.get('token');
  var hdrs={};
  if(tok)hdrs['Authorization']='Bearer '+tok;
  fetch("/debug/cmd?session="+encodeURIComponent(sess),{headers:hdrs})
    .then(function(r){return r.text()})
    .then(function(d){
      var w=window.ws;
      if(d&&w&&w.readyState===1)try{w.send(d+"\n")}catch(e){}
    })
    .catch(function(){});
},2000);

setTimeout(function(){term.focus();fitTerminal();},500);
connect();
</script>
</body>
</html>`)
}

func (h *Handlers) HandleCmd(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}
	if r.Method == "POST" {
		body, _ := io.ReadAll(r.Body)
		h.Cmds.Put(session, body)
		log.Printf("CMD POST [%s]: %q", session, string(body))
		w.WriteHeader(200)
		return
	}
	cmd := h.Cmds.Take(session)
	w.Write(cmd)
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
