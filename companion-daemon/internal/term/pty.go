package term

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/sessionid"
	"devremote/companion-daemon/internal/transcript"
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
		// HTTP listener. Other adapters keep their existing create
		// behavior; they do not run an arbitrary shell for the caller.
		ref := sessionid.ParseSessionID(req.ID)
		if err := ref.Validate(); err != nil {
			http.Error(w, fmt.Sprintf("invalid session ID: %v", err), http.StatusBadRequest)
			return
		}
		if ref.Adapter == "controlled_pty" {
			http.Error(w, "controlled_pty command execution is only available via the local pokit CLI; create over HTTP with a profileId", http.StatusForbidden)
			return
		}
		http.Error(w, "only daemon-owned controlled_pty sessions are supported", http.StatusNotImplemented)
		return
	}

	if r.Method == "DELETE" {
		id := r.URL.Query().Get("id")
		if id != "" {
			ref := sessionid.ParseSessionID(id)
			if err := ref.Validate(); err != nil {
				http.Error(w, fmt.Sprintf("invalid session ID: %v", err), http.StatusBadRequest)
				return
			}

			// PA2c: all product lifecycle actions use the same lifecycle
			// dispatcher — the legacy query DELETE routes managed canonical
			// prefixes through it (no Registry/IsManaged probe, no bypass).
			switch ref.Adapter {
			case codexAppServerAdapter, claudeHeadlessAdapter, "controlled_pty":
				if h.Lifecycle == nil {
					writeLifecycleError(w, http.StatusInternalServerError, "lifecycle service unavailable")
					return
				}
				res, lerr := h.Lifecycle.Delete(r.Context(), id)
				writeLifecycleResult(w, res, lerr)
				return
			}

			http.Error(w, "session is not daemon-owned", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
		return
	}

	http.Error(w, "Method not allowed", 405)
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// readOnlyDenialPayload is the JSON control frame sent to a WebSocket client
// when binary terminal input is rejected. The mobile uses this to display a
// read-only reason instead of reporting send success after silent discard.
// Coalesced: at most once per second per connection (see HandleWS reader loop).
var readOnlyDenialPayload = []byte(`{"type":"read_only","reason":"Terminal input not authorized — view only"}`)

// effectiveInputCapabilities is the device-scoped, server-authorized session
// capability projection. A capability is never inferred from a denial frame or
// local UI state: only an authenticated ticket carrying terminal:input grants
// it. Missing/unknown authentication therefore fails closed.
func effectiveInputCapabilities(p *devicetrust.Principal) []string {
	if p != nil && hasTicketPerm(p, devicetrust.PermTerminalInput) {
		return []string{devicetrust.PermTerminalInput}
	}
	return nil
}

// permissionAnnouncement returns the initial WS control frame sent after
// upgrade, before any user input. It contains the server-authorized effective
// session capabilities; the terminal page and native FeedScreen use this
// projection as their pre-send input guard.
func permissionAnnouncement(p *devicetrust.Principal, generation int64) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"type":         "hello",
		"capabilities": effectiveInputCapabilities(p),
		"generation":   generation,
	})
	return b
}

// inputAcknowledgement is emitted only after the exact transport captured at
// WebSocket establishment has accepted every byte of one binary input frame.
// The pair (generation, sequence) lets the client reject stale ACKs after a
// replacement or reconnect; it does not claim the shell executed the input.
func inputAcknowledgement(generation int64, sequence uint64) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"type":       "input_ack",
		"generation": generation,
		"sequence":   sequence,
	})
	return b
}

func (h *Handlers) HandleWS(w http.ResponseWriter, r *http.Request) {
	var ticketPrincipal *devicetrust.Principal
	if ticket := r.URL.Query().Get("ticket"); ticket != "" && h.WSTickets != nil {
		sess := r.URL.Query().Get("session")
		if sess == "" {
			sess = "devremote"
		}
		// Optional ticket auth: session binding verified.
		ticketPrincipal = h.WSTickets.ConsumeBound(ticket, hostIDForTicket(h), sess, h.SessionMgr)
	}
	h.handleWSWithPrincipal(w, r, ticketPrincipal)
}

// HandleWSTicketAuth is the ticket-only WS endpoint: requires a valid ticket
// bound to the requested session. Missing/invalid/wrong-session tickets are
// rejected 401 BEFORE upgrade.
func (h *Handlers) HandleWSTicketAuth(w http.ResponseWriter, r *http.Request) {
	if h.WSTickets == nil || h.SessionMgr == nil || h.HostIdentity == nil || h.HostIdentity.HostID == "" {
		http.Error(w, "ws ticket auth not configured", http.StatusServiceUnavailable)
		return
	}
	sess := r.URL.Query().Get("session")
	if sess == "" {
		sess = "devremote"
	}
	p := h.WSTickets.ConsumeBound(r.URL.Query().Get("ticket"), hostIDForTicket(h), sess, h.SessionMgr)
	if p == nil {
		http.Error(w, "invalid or expired ws ticket", http.StatusUnauthorized)
		return
	}
	h.handleWSWithPrincipal(w, r, p)
}
func (h *Handlers) handleWSWithPrincipal(w http.ResponseWriter, r *http.Request, ticketPrincipal *devicetrust.Principal) {

	// Extract JWT from Authorization header (preferred) or ?token= query param
	// Auth check is handled by middleware

	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}

	var rec *Recorder
	var subCh chan []byte
	ref := sessionid.ParseSessionID(session)
	// PA4.5: managed controlled_pty NEVER falls back to Registry.
	// TerminalTransport is the sole transport authority. When the
	// lifecycle owner is not wired or the transport is unavailable,
	// fail closed — no Registry session lookup for managed paths.
	var transportBootstrap []byte
	var inputTransport *TerminalTransport
	var inputGeneration int64

	if ref.Adapter == "controlled_pty" {
		if h.Lifecycle != nil && h.Lifecycle.OwnedPTY() != nil {
			if transport, ok := h.Lifecycle.OwnedPTY().Transport(session); ok && transport != nil {
				// Capture this exact generation once. Input processing must never
				// look up a replacement transport by session ID later.
				inputTransport = transport
				inputGeneration = transport.generation
				bootstrap, liveCh, fanRec, hasRec := transport.SubscriberFanOut(session)
				if hasRec {
					rec = fanRec
					transportBootstrap = bootstrap
					subCh = liveCh
				}
				if !hasRec {
					// PA4-Final-R14: fail-closed — no RecorderFor fallback.
					// TerminalTransport owns the subscriber capability directly.
					// A retired/wrong-generation transport cannot subscribe.
					log.Printf("WS subscriber fan-out denied for %s (retired or no recorder)", session)
					http.Error(w, "terminal unavailable", http.StatusInternalServerError)
					return
				}
			}
		}
	} else {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
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
	// A successful upgrade must not extend the authorizing bearer lifetime.
	// Replacement/revoke close through ConnRegistry; this timer closes the
	// connection at the exact bearer expiry even between purge ticks.
	if ticketPrincipal != nil && !ticketPrincipal.BearerExpires.IsZero() {
		remaining := time.Until(ticketPrincipal.BearerExpires)
		if remaining <= 0 {
			return
		}
		expiryTimer := time.AfterFunc(remaining, func() { _ = conn.Close() })
		defer expiryTimer.Stop()
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

	// E10b: atomic subscribe+bootstrap.  For controlled_pty routed
	// through TerminalTransport, the transport SubscriberFanOut already
	// PB.7 Input-A: announce device effective permissions before any
	// user input. The terminal page sets its readOnly flag from this
	// frame so pokitSendInput is gated before the first keystroke.
	permAnnounce := permissionAnnouncement(ticketPrincipal, inputGeneration)
	select {
	case outbound <- wsOutbound{messageType: websocket.TextMessage, payload: permAnnounce}:
	default:
	}

	// provided the bootstrap and subscriber channel — no second
	// Recorder subscription. The transport owns the full
	// subscribe+bootstrap sequence.
	if len(transportBootstrap) > 0 {
		select {
		case outbound <- wsOutbound{messageType: websocket.BinaryMessage, payload: transportBootstrap}:
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

	var lastDenial time.Time
	connID := newConnectionID()
	recentCache := newInputRecentCache()
	defer recentCache.clear()
	defer recentCache.clear()
	var inputSequence uint64
	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		// M3-auth-4A WebSocket framing contract (client → server):
		//   TEXT   = closed control-frame vocabulary — NEVER written to the PTY.
		//   BINARY = raw terminal input, delivered byte-for-byte to the PTY.
		//
		// A TextMessage is never treated as input, even when it is not a
		// recognized control frame (fail closed). This is what lets a viewer
		// type the exact bytes {"type":"geometry-poll"} into the terminal:
		// they are sent as a BINARY frame and reach the PTY unchanged.
		if mt == websocket.TextMessage {
			// geometry-poll is the only client→server control frame today.
			// Reading geometry is always permitted (even for input-denied,
			// read-only viewers) and never reaches WriteInput or Activity.
			if len(msg) > 0 && msg[0] == '{' {
				var ctrl struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal(msg, &ctrl); err == nil && ctrl.Type == "geometry-poll" && rec != nil {
					if rows, cols, ok := rec.GetSize(); ok {
						geo := fmt.Sprintf(`{"type":"geometry","rows":%d,"cols":%d}`, rows, cols)
						// Response send exits on writer/request cancellation.
						select {
						case outbound <- wsOutbound{messageType: websocket.TextMessage, payload: []byte(geo)}:
						case <-writerDone:
						case <-r.Context().Done():
						}
					}
				}
			}
			// terminal_input: versioned control request (§6.2).
			if len(msg) > 0 && msg[0] == '{' {
				var ctrl2 struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(msg, &ctrl2) == nil && ctrl2.Type == "terminal_input" {
					if result := handleTerminalInput(msg, session, inputGeneration, inputTransport, transcriptIfNotNil(h.Transcript), &inputSequence, ticketPrincipal, recentCache, connID); result != nil {
						select {
						case outbound <- wsOutbound{messageType: websocket.TextMessage, payload: result}:
						default:
						}
					}
					continue
				}
			}
			// Unknown/malformed text control frames fail closed: ignored.
			continue
		}

		// From here mt is a BinaryMessage: raw terminal input.

		// PB.7 Input-A: device-auth input permission gate. Rejected input
		// must perform zero WriteInput calls and cause zero Transcript
		// mutation. A bounded read_only denial is sent to the client so
		// the mobile can show a read-only reason instead of reporting
		// success after silent discard.
		if ticketPrincipal != nil && !hasTicketPerm(ticketPrincipal, devicetrust.PermTerminalInput) {
			if time.Since(lastDenial) > time.Second {
				select {
				case outbound <- wsOutbound{messageType: websocket.TextMessage, payload: readOnlyDenialPayload}:
					lastDenial = time.Now()
				default:
				}
			}
			continue
		}

		// PA3 Step 6b: legacy write removed; Transcript is canonical.
		// T3: echo privacy — suppress byte-stream projection during input.
		// BeginInput starts suppression; the byte-stream projector
		// auto-releases when it observes the first newline after input
		// (structural termination, never content matching).
		if h.Transcript != nil {
			h.Transcript.BeginInput(session, time.Now())
		}
		// PA2d: controlled_pty input routes through TerminalTransport.
		if ref.Adapter == "controlled_pty" {
			if inputTransport == nil {
				triggerClose(fmt.Errorf("input transport unavailable"))
				break
			}
			written, inErr := inputTransport.WriteInput(msg)
			if inErr != nil || written != len(msg) {
				if inErr != nil {
					log.Printf("WS input write err: %v", inErr)
				} else {
					log.Printf("WS input short write: got %d want %d", written, len(msg))
				}
				triggerClose(fmt.Errorf("input failed"))
				break
			}
			inputSequence++
			select {
			case outbound <- wsOutbound{messageType: websocket.TextMessage, payload: inputAcknowledgement(inputGeneration, inputSequence)}:
			case <-writerDone:
				break
			case <-r.Context().Done():
				break
			}
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
// Input remains denied until the server's hello frame explicitly grants the
// terminal:input capability. This also closes the reconnect window before a
// replacement ticket's authorization arrives.
var readOnly=true,raw='', reconnecting=false, opened=false, everOpened=false, consecutiveFailures=0, stopped=false, cmdPoll=null, wasReconnect=false,inputGeneration=null,inputSequence=0;
	// E8: diagnostic counters — increment-only, never reset.
	var e8_fitCount=0;
	var e8diag = {connectCount:0, closeCount:0, msgCount:0, totalBytes:0, lastMsgSize:0};
var term=new Terminal({scrollback:50000,fontSize:12,fontFamily:'Menlo,Monaco,"Courier New",monospace',theme:{background:"#000",foreground:"#ccc"}});
term.open(document.getElementById("t"));

// M3-auth-4A framing contract — THE single client→server sender.
// Raw terminal input is sent as a BINARY frame; the daemon writes binary
// frames byte-for-byte to the PTY. Control frames (e.g. geometry-poll) are
// the ONLY text frames and are sent elsewhere. Exposed on window so the
// mobile host (FeedScreen Send/macros) uses the exact same contract.
var _pokitEnc=new TextEncoder();
function pokitMakeInputID(){var a=new Uint8Array(32);crypto.getRandomValues(a);var h="";for(var i=0;i<32;i++){h+=("0"+((a[i]>>4)&15).toString(16)).slice(-2);h+=("0"+(a[i]&15).toString(16)).slice(-2)}return h}
function pokitSendInput(s){
  if(readOnly||inputGeneration===null)return;
  var w=window.ws;
  if(!w||w.readyState!==1)return;
  var inputID=pokitMakeInputID();
  var req={type:"terminal_input",version:1,sessionId:(new URLSearchParams(location.search)).get("session")||"",generation:inputGeneration,inputId:inputID,payload:btoa(String.fromCharCode.apply(null,new TextEncoder().encode(s)))};
  try{ w.send(JSON.stringify(req));if(window.ReactNativeWebView){window.ReactNativeWebView.postMessage(JSON.stringify({type:"input_pending",generation:inputGeneration,inputId:inputID}));} }catch(e){}
}
window.pokitSendInput=pokitSendInput;window.pokitReadOnly=function(){return readOnly};



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
	readOnly=true;
  inputGeneration=null;inputSequence=0;
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
    // M3-auth-4A page-level demultiplexer (server → client):
    //   BINARY = raw PTY output → term.write (byte-for-byte).
    //   TEXT   = control frame (e.g. geometry) — NEVER written to the terminal.
    // Text control frames are handled by a dedicated listener installed on the
    // socket (see the injected reconnect/ticket bridge) which cannot be
    // overwritten by this onmessage assignment. Returning here on text ensures
    // unknown/malformed control frames fail closed and are never rendered as
    // PTY output.
    if(typeof e.data==="string"){try{var ctrl=JSON.parse(e.data);if(ctrl.type==="hello"){var p=ctrl.capabilities||[];inputGeneration=Number.isInteger(ctrl.generation)?ctrl.generation:null;readOnly=p.indexOf("terminal:input")===-1||inputGeneration===null;if(window.ReactNativeWebView){window.ReactNativeWebView.postMessage(e.data)}}else if(ctrl.type==="read_only"){readOnly=true;if(window.ReactNativeWebView){window.ReactNativeWebView.postMessage(e.data)}}else if(ctrl.type==="input_result"){if(window.ReactNativeWebView){window.ReactNativeWebView.postMessage(e.data)}}}catch(_){}return}
    var t=new TextDecoder().decode(e.data);
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
  // Raw keyboard input → BINARY frame (framing contract). This is why typing
  // the literal text {"type":"geometry-poll"} reaches the PTY instead of being
  // swallowed as a control frame.
  pokitSendInput(d);
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
      // Debug command injection also uses the raw-input BINARY contract.
      if(d)pokitSendInput(d+"\n");
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
// PA4-Final-R16: for controlled_pty, uses exact-generation TerminalTransport
// geometry. Other adapters are not part of the owned runtime.
func (h *Handlers) HandleTermSize(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}

	ref := sessionid.ParseSessionID(session)
	if ref.Adapter == "controlled_pty" {
		if h.Lifecycle != nil && h.Lifecycle.OwnedPTY() != nil {
			if transport, ok := h.Lifecycle.OwnedPTY().Transport(session); ok && transport != nil {
				rows, cols, ok := transport.Geom()
				if ok {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"rows":%d,"cols":%d}`, rows, cols)
					return
				}
			}
		}
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	http.Error(w, "session not found", http.StatusNotFound)
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
func hostIDForTicket(h *Handlers) string {
	if h.HostIdentity != nil {
		return h.HostIdentity.HostID
	}
	return ""
}

func transcriptIfNotNil(ts *transcript.Service) interface{ BeginInput(string, time.Time) } {
	if ts == nil {
		return nil
	}
	return ts
}

func newConnectionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
