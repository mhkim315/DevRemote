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

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/mux"
	"github.com/gorilla/websocket"
)

// hasTicketPerm reports whether a device-authenticated principal has a given
// permission. Returns true if principal is nil (legacy / non-device-auth path).
func hasTicketPerm(p *devicetrust.Principal, need string) bool {
	if p == nil {
		return true
	}
	for _, perm := range p.Permissions {
		if perm == need {
			return true
		}
	}
	return false
}

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
		var req createSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// PUT keeps the legacy no-op acknowledgement.
		if r.Method == http.MethodPut {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{"status":"ok"}`))
			return
		}

		// M1 safe create: profileId present -> daemon-owned launch. The daemon
		// generates the canonical ID and resolves the executable by policy.
		if req.ProfileID != "" {
			h.createFromProfile(w, r, req)
			return
		}

		// Legacy shape (client-supplied id + optional command). controlled_pty
		// command execution runs an arbitrary `bash -c` and is a privileged
		// LOCAL operation over the 0600 socket — never over the tunnel-reachable
		// HTTP listener. Other adapters (tmux/cmux) keep their existing create
		// behavior; they do not run an arbitrary shell for the caller.
		ref := mux.ParseSessionID(req.ID)
		if err := ref.Validate(); err != nil {
			http.Error(w, fmt.Sprintf("invalid session ID: %v", err), http.StatusBadRequest)
			return
		}
		if ref.Adapter == "controlled_pty" {
			http.Error(w, "controlled_pty command execution is only available via the local pokit CLI; create over HTTP with a profileId", http.StatusForbidden)
			return
		}
		// Strict decode: a present command must be a JSON string. Never coerce
		// malformed input into a default shell.
		var cmdStr string
		if len(req.Command) > 0 {
			if err := json.Unmarshal(req.Command, &cmdStr); err != nil {
				http.Error(w, "command must be a string", http.StatusBadRequest)
				return
			}
		}
		opts := mux.CreateOptions{Name: ref.LocalID, WorkspaceID: req.WorkspaceID, Command: cmdStr, CWD: req.CWD}
		createdID, err := reg.CreateSession(r.Context(), ref.Adapter, opts)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to create session: %v", err), http.StatusInternalServerError)
			return
		}
		canonicalID := mux.SessionRef{Adapter: ref.Adapter, LocalID: createdID}.Canonical()
		// Best-effort recorder start for external/streamable adapters; also
		// drops the starter subscriber so no phantom viewer is retained.
		_, _ = startRecorder(r.Context(), reg, h.Activity, canonicalID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(fmt.Sprintf(`{"status":"ok","id":"%s"}`, canonicalID)))
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

			// Managed sessions must go through the M2 lifecycle contract
			// (managedLifecycle gate, terminal-state rule, history retention);
			// the legacy query DELETE must not bypass it to kill a running
			// process or clear history.
			if h.Lifecycle != nil && h.Lifecycle.IsManaged(r.Context(), id) {
				res, lerr := h.Lifecycle.Delete(r.Context(), id)
				writeLifecycleResult(w, res, lerr)
				return
			}

			// External adapters keep the legacy compatibility behavior.
			adapterName := ref.Adapter

			if err := reg.TerminateSession(r.Context(), adapterName, ref.LocalID); err != nil {
				http.Error(w, fmt.Sprintf("failed to terminate session: %v", err), http.StatusInternalServerError)
				return
			}
			DeleteRecorder(id)
			if h.Activity != nil {
				h.Activity.Clear(id)
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
	var ticketPrincipal *devicetrust.Principal
	if ticket := r.URL.Query().Get("ticket"); ticket != "" && h.WSTickets != nil {
		sess := r.URL.Query().Get("session")
		if sess == "" {
			sess = "devremote"
		}
		// Optional ticket auth: session binding verified.
		ticketPrincipal = h.WSTickets.ConsumeBound(ticket, "", sess, h.SessionMgr)
	}
	h.handleWSWithPrincipal(w, r, ticketPrincipal)
}

// HandleWSTicketAuth is the ticket-only WS endpoint: requires a valid ticket
// bound to the requested session. Missing/invalid/wrong-session tickets are
// rejected 401 BEFORE upgrade.
func (h *Handlers) HandleWSTicketAuth(w http.ResponseWriter, r *http.Request) {
	if h.WSTickets == nil {
		http.Error(w, "ws ticket auth not configured", http.StatusServiceUnavailable)
		return
	}
	sess := r.URL.Query().Get("session")
	if sess == "" {
		sess = "devremote"
	}
	p := h.WSTickets.ConsumeBound(r.URL.Query().Get("ticket"), "", sess, h.SessionMgr)
	if p == nil {
		http.Error(w, "invalid or expired ws ticket", http.StatusUnauthorized)
		return
	}
	h.handleWSWithPrincipal(w, r, p)
}
func (h *Handlers) handleWSWithPrincipal(w http.ResponseWriter, r *http.Request, ticketPrincipal *devicetrust.Principal) {
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

	// E8g4: cmux uses screen snapshots, not PTY byte stream.
	// Live terminal rendering accumulates xterm scrollback via ESC[2J.
	// Disable live terminal for screen_snapshot_delta adapters (P0 fix).
	if adapter, ok := reg.Adapter(s.AdapterName()); ok {
		if cp, ok := adapter.(mux.TranscriptCaptureProvider); ok &&
			cp.TranscriptCaptureMode() == mux.CaptureModeScreenSnapshotDelta {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			w.Write([]byte(`{"error":"unsupported","detail":"cmux uses screen snapshots. Live terminal is disabled for this adapter. Use Transcript tab for captured output."}`))
			return
		}
	}

	// E8f2: subscribe to session recorder. Recorder opens stream ONCE.
	opener, hasStream := s.(mux.StreamOpener)
	if !hasStream {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"unsupported","detail":"session does not support live streaming"}`))
		return
	}
	rec, subCh := EnsureRecorder(session, opener, h.Activity)
	if rec == nil {
		http.Error(w, "stream failed", 500)
		return
	}
	defer func() {
		rec.Unsubscribe(subCh)
		// Stop recorder only if no subscribers remain and stream is done.
		if rec.Err() != nil {
			rec.Stop()
		}
	}()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS upgrade err: %v", err)
		return
	}
	defer conn.Close()

	if h.ConnRegistry != nil && ticketPrincipal != nil {
		h.ConnRegistry.Register(ticketPrincipal.DeviceID, conn)
		defer h.ConnRegistry.Unregister(ticketPrincipal.DeviceID, conn)
	}

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

	if sr, ok := s.(mux.ScreenReader); ok {
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

	// E10b: atomic subscribe+bootstrap prevents gap/duplicate.
	// Replace subCh from EnsureRecorder with atomic handoff.
	rec.Unsubscribe(subCh)
	bootstrap, subCh := rec.SubscribeWithBootstrap()
	if len(bootstrap) > 0 {
		select {
		case outbound <- wsOutbound{messageType: websocket.BinaryMessage, payload: bootstrap}:
		case <-writerDone:
			return
		case <-r.Context().Done():
			return
		}
	}
	// E8f2: read from recorder broadcast instead of own PTY stream.
	go func() {
		for {
			select {
			case payload, ok := <-subCh:
				if !ok {
					// Recorder closed — propagate error.
					triggerClose(fmt.Errorf("stream failed"))
					return
				}
				select {
				case outbound <- wsOutbound{messageType: websocket.BinaryMessage, payload: payload}:
				case <-writerDone:
					return
				case <-r.Context().Done():
					return
				}
			case <-r.Context().Done():
				return
			case <-writerDone:
				return
			}
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		// M2.5-4: device-auth input permission gate. Rejected input must not
		// reach WriteInput OR modify Activity.
		if ticketPrincipal != nil && !hasTicketPerm(ticketPrincipal, devicetrust.PermTerminalInput) {
			continue
		}

		// E8f: capture terminal input safely.
		if h.Activity != nil && len(msg) > 0 {
			h.Activity.Append(ActivityEvent{
				SessionID: session,
				Type:      ActivityTerminalInput,
				Text:      "",
				Bytes:     len(msg),
			})
		}
		if writer, ok := s.(mux.InputWriter); ok {
			if inErr := writer.WriteInput(r.Context(), msg); inErr != nil {
				log.Printf("WS input write err: %v", inErr)
				triggerClose(fmt.Errorf("input failed"))
				break
			}
		} else if rec != nil {
			rec.WriteInput(msg)
		}
	}

	// E8f2: recorder owns stream lifecycle. Last subscriber exit may stop recorder.
	if rec.Err() != nil {
		rec.Stop()
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
var raw='', reconnecting=false, opened=false, everOpened=false, consecutiveFailures=0, stopped=false, cmdPoll=null, wasReconnect=false;
	// E8: diagnostic counters — increment-only, never reset.
	var e8_fitCount=0;
	var e8diag = {connectCount:0, closeCount:0, msgCount:0, totalBytes:0, lastMsgSize:0};
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
    everOpened=true;
	    e8diag.connectCount++;
    consecutiveFailures=0;
    reconnecting=false;
		    // E8: clear terminal on reconnect to prevent scroll duplication.
		    if(wasReconnect){ term.clear(); wasReconnect=false; }
    document.getElementById('status').style.display='none';
    setTimeout(function(){e8_fitCount++;fitTerminal()},500);
  };
  ws.onmessage=function(e){
    var t=typeof e.data==='string'?e.data:new TextDecoder().decode(e.data);
    raw+=t;
	    e8diag.msgCount++; e8diag.totalBytes+=t.length; e8diag.lastMsgSize=t.length; e8diag.rawLen=raw.length;
    // E8: if user scrolled up, show new-output badge instead of forcing viewport.
	    term.write(t);
  };
  ws.onclose=function(e){
    if(stopped)return;
    if(!opened) consecutiveFailures++;
    // Explicit end from the daemon (session gone / auth) — do not retry.
    if(e.code===1008||e.code===1011){
      stopSession('Session ended or unavailable');
      return;
    }
    // Give up only if we have NEVER connected (bad session/URL or a long
    // initial outage): bounded retry, then surface failure. Once we've
    // connected at least once, a later close is a transient network drop —
    // keep retrying indefinitely with backoff until the network returns.
    if(!everOpened && consecutiveFailures>=6){
      stopSession('Cannot connect');
      return;
    }
    if(!reconnecting){
      reconnecting=true;
		      e8diag.closeCount++; wasReconnect=true;
      setStatus('reconnecting...');
      var delay=Math.min(1500*Math.pow(1.6,Math.min(consecutiveFailures,6)),15000);
      setTimeout(function(){reconnecting=false;connect()},delay);
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
	// E8: prevent mobile scroll duplication by resetting viewport after scroll.
	var e8_scrollTimer = null;
	var e8_wasScrolled = false;
	term.onScroll(function(pos) {
	  e8_wasScrolled = true;
	  if(e8_scrollTimer) clearTimeout(e8_scrollTimer);
	  e8_scrollTimer = setTimeout(function(){
	    // After scroll settles, re-render the viewport to fix any visual duplication.
	    if (typeof term.viewport !== 'undefined' && term.viewport) {
	      
	    }
	    e8_wasScrolled = false;
	  }, 300);
	});
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
	// E8: post diagnostic counters to React Native every 5s.
	setInterval(function(){
	  var diag = {
	    type:"e8diag",
	    connectCount: e8diag.connectCount,
	    closeCount: e8diag.closeCount,
	    msgCount: e8diag.msgCount,
	    totalBytes: e8diag.totalBytes,
	    lastMsgSize: e8diag.lastMsgSize,
	    rawLen: e8diag.rawLen || raw.length,
	    fitCount: e8_fitCount, wasReconnect: wasReconnect
	  };
	  // Route 1: to React Native via postMessage.
	  try{
	    if(window.ReactNativeWebView){
	      window.ReactNativeWebView.postMessage(JSON.stringify(diag));
	    }
	  }catch(e){}
	  // Route 2: to daemon log via authenticated diagnostic POST.
	  var qs = Object.keys(diag).map(function(k){return k+'='+encodeURIComponent(diag[k])}).join('&');
		  var p=new URLSearchParams(location.search);
		  var tok=p.get('token');
		  var hdrs={};
		  if(tok)hdrs['Authorization']='Bearer '+tok;
		  fetch('/debug/e8diag?'+qs,{method:'POST',headers:hdrs}).catch(function(){});
	},5000);
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

// isANSIControlOnly returns true if the text is only ANSI escapes or control chars.
func isANSIControlOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 32 && c != 0x1b {
			return false // printable char found
		}
	}
	return true // all control/ANSI bytes
}

// stripANSI removes ANSI escape sequences from text so Transcript renders
// readable output instead of raw terminal control codes.
// Handles CSI (ESC[ ... letter), OSC (ESC] ... BEL/ST), and other ESC-based sequences.
func stripANSI(s string) string {
	var out []byte
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) {
			switch s[i+1] {
			case '[': // CSI: ESC[ ... 0x40-0x7E
				j := i + 2
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					j++ // consume the terminating byte
				}
				i = j
				continue
			case ']': // OSC: ESC] ... BEL(0x07) or ST(ESC\)
				j := i + 2
				for j < len(s) && s[j] != 0x07 && !(s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\') {
					j++
				}
				if j < len(s) {
					if s[j] == 0x07 {
						j++
					} else {
						j += 2 // ESC \
					}
				}
				i = j
				continue
			default:
				// Other ESC sequences: ESC7, ESC8, ESC=, ESC>, ESC(, etc.
				// These are 2 bytes: ESC + command byte.
				i += 2
				continue
			}
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
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

// HandleTermSize (GET) returns the PTY's current geometry for a session as
// JSON {"rows":R,"cols":C}. Mobile viewers mirror this so full-width TUIs
// (e.g. claude drawing to the alternate screen at the PTY width) render
// without re-wrapping to the phone width.
func HandleTermSize(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}
	rec := GetRecorder(session)
	if rec == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	rows, cols, ok := rec.GetSize()
	if !ok {
		http.Error(w, "size unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"rows":%d,"cols":%d}`, rows, cols)
}

// HandleE8Diag receives diagnostic counters from the terminal WebView.
// Logs them to daemon stdout — capturable without Metro or adb logcat.
// POST-only, accepts only numeric/bool fields, truncates long values.
func HandleE8Diag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	safeNum := func(key string) string {
		v := q.Get(key)
		for _, c := range v {
			if c < '0' || c > '9' {
				return "0"
			}
		}
		if len(v) > 20 {
			v = v[:20]
		}
		if v == "" {
			v = "0"
		}
		return v
	}
	safeBool := func(key string) string {
		v := q.Get(key)
		if v == "true" || v == "false" {
			return v
		}
		return "false"
	}
	log.Printf("E8DIAG connectCount=%s closeCount=%s msgCount=%s totalBytes=%s lastMsgSize=%s rawLen=%s wasReconnect=%s",
		safeNum("connectCount"), safeNum("closeCount"), safeNum("msgCount"),
		safeNum("totalBytes"), safeNum("lastMsgSize"), safeNum("rawLen"), safeBool("wasReconnect"))
	w.WriteHeader(200)
}
