package term

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"github.com/dop251/goja"
	"github.com/gorilla/websocket"
)

// servedPageHarness executes the literal HTML returned by HandleHTML in Goja.
// Browser primitives are minimal host boundaries only: Terminal records output,
// WebSocket records browser->daemon frames, and ReactNativeWebView records the
// actual page bridge's postMessage payloads. The control bridge itself is never
// reproduced in Go.
type servedPageHarness struct {
	t        *testing.T
	vm       *goja.Runtime
	socket   *goja.Object
	oldSock  *goja.Object
	onData   goja.Callable
	sent     []string
	posts    []map[string]any
	writes   []string
	dial     func() *websocket.Conn
	live     map[*goja.Object]*websocket.Conn
	timers   []goja.Callable
	delivery *c1NativeDelivery
}

func newServedPageHarness(t *testing.T) *servedPageHarness {
	t.Helper()
	rr := httptest.NewRecorder()
	(&Handlers{}).HandleHTML(rr, httptest.NewRequest("GET", "http://daemon.test/term/?session=controlled_pty:goja", nil))
	if rr.Code != 200 {
		t.Fatalf("HandleHTML status=%d", rr.Code)
	}
	return newServedPageHarnessHTML(t, rr.Body.String(), "controlled_pty:goja", nil)
}

// newLiveServedPageHarness executes the served page with a WebSocket object
// whose send method writes to the actual ticketed HandleWS endpoint. Server
// frames are synchronously pumped into the page's real onmessage callback.
func newLiveServedPageHarness(t *testing.T, f *c1Fixture) *servedPageHarness {
	t.Helper()
	return newServedPageHarnessHTML(t, f.pageHTML, f.session, func() *websocket.Conn { return f.dialWS(t) })
}

func newServedPageHarnessHTML(t *testing.T, html, session string, dial func() *websocket.Conn) *servedPageHarness {
	t.Helper()
	matches := regexp.MustCompile(`(?s)<script>\s*(.*?)</script>\s*</body>`).FindStringSubmatch(html)
	if len(matches) != 2 {
		t.Fatal("served inline terminal script not found")
	}

	h := &servedPageHarness{t: t, vm: goja.New(), dial: dial, live: make(map[*goja.Object]*websocket.Conn), delivery: newC1NativeDelivery()}
	vm := h.vm
	window := vm.GlobalObject()
	if err := vm.Set("window", window); err != nil {
		t.Fatal(err)
	}
	location := vm.NewObject()
	location.Set("protocol", "http:")
	location.Set("host", "daemon.test")
	location.Set("search", "?session="+session)
	window.Set("location", location)

	status := vm.NewObject()
	status.Set("style", vm.NewObject())
	target := vm.NewObject()
	document := vm.NewObject()
	document.Set("getElementById", func(call goja.FunctionCall) goja.Value {
		if call.Argument(0).String() == "status" {
			return status
		}
		return target
	})
	document.Set("addEventListener", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	window.Set("document", document)
	window.Set("addEventListener", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	window.Set("setTimeout", func(call goja.FunctionCall) goja.Value {
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			h.timers = append(h.timers, fn)
		}
		return vm.ToValue(len(h.timers))
	})
	window.Set("setInterval", func(goja.FunctionCall) goja.Value { return vm.ToValue(1) })
	window.Set("clearInterval", func(goja.FunctionCall) goja.Value { return goja.Undefined() })

	if _, err := vm.RunString(`
var __termC1Entropy=0;
var crypto={getRandomValues:function(a){for(var i=0;i<a.length;i++)a[i]=(__termC1Entropy+i)&255;__termC1Entropy++;return a;}};
function TextEncoder(){};
TextEncoder.prototype.encode=function(s){var a=[];for(var i=0;i<s.length;i++)a.push(s.charCodeAt(i)&255);return a;};
function TextDecoder(){};
TextDecoder.prototype.decode=function(a){var s='';for(var i=0;i<a.length;i++)s+=String.fromCharCode(a[i]);return s;};
`); err != nil {
		t.Fatal(err)
	}
	window.Set("btoa", func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) })

	if err := vm.Set("Terminal", func(call goja.ConstructorCall) *goja.Object {
		term := vm.NewObject()
		term.Set("open", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		term.Set("focus", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		term.Set("clear", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		term.Set("resize", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		term.Set("write", func(call goja.FunctionCall) goja.Value {
			h.writes = append(h.writes, call.Argument(0).String())
			return goja.Undefined()
		})
		term.Set("writeln", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		term.Set("onScroll", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		term.Set("onData", func(call goja.FunctionCall) goja.Value {
			fn, ok := goja.AssertFunction(call.Argument(0))
			if !ok {
				t.Fatal("served term.onData callback is not callable")
			}
			h.onData = fn
			return goja.Undefined()
		})
		return term
	}); err != nil {
		t.Fatal(err)
	}
	if err := vm.Set("WebSocket", func(call goja.ConstructorCall) *goja.Object {
		if h.socket != nil {
			h.oldSock = h.socket
		}
		socket := vm.NewObject()
		socket.Set("readyState", 1)
		if h.dial != nil {
			h.live[socket] = h.dial()
		}
		socket.Set("send", func(call goja.FunctionCall) goja.Value {
			raw := call.Argument(0).String()
			h.sent = append(h.sent, raw)
			if conn := h.live[socket]; conn != nil {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(raw)); err != nil {
					t.Fatalf("served WebSocket.send → HandleWS: %v", err)
				}
			}
			return goja.Undefined()
		})
		socket.Set("close", func(goja.FunctionCall) goja.Value {
			h.closeLiveSocket(socket)
			return goja.Undefined()
		})
		h.socket = socket
		return socket
	}); err != nil {
		t.Fatal(err)
	}
	rn := vm.NewObject()
	rn.Set("postMessage", func(call goja.FunctionCall) goja.Value {
		var payload map[string]any
		if err := json.Unmarshal([]byte(call.Argument(0).String()), &payload); err != nil {
			t.Fatalf("ReactNativeWebView payload=%q: %v", call.Argument(0).String(), err)
		}
		h.posts = append(h.posts, payload)
		h.delivery.consume(payload)
		return goja.Undefined()
	})
	window.Set("ReactNativeWebView", rn)

	if _, err := vm.RunString(matches[1]); err != nil {
		t.Fatalf("served page execution failed: %v", err)
	}
	if h.socket == nil || h.onData == nil {
		t.Fatal("served page did not establish WebSocket and direct-keyboard handler")
	}
	return h
}

// pumpText drives a TEXT control generated by the real daemon through the
// page's actual ws.onmessage handler. It deliberately does not fabricate ACKs.
func (h *servedPageHarness) pumpText(socket *goja.Object) string {
	h.t.Helper()
	conn := h.live[socket]
	if conn == nil {
		h.t.Fatal("pumpText requires a live HandleWS socket")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn.SetReadDeadline(deadline)
		kind, payload, err := conn.ReadMessage()
		if err != nil {
			h.t.Fatalf("read HandleWS control frame: %v", err)
		}
		if kind != websocket.TextMessage {
			continue
		}
		raw := string(payload)
		h.emitRaw(socket, raw)
		return raw
	}
}

// closeLiveSocket is the browser boundary for a network close: it closes the
// real gorilla peer and invokes the page's actual onclose callback. Timers are
// then run explicitly by the test, keeping reconnect timing deterministic.
func (h *servedPageHarness) closeLiveSocket(socket *goja.Object) {
	if conn := h.live[socket]; conn != nil {
		_ = conn.Close()
	}
	socket.Set("readyState", 3)
	if onClose, ok := goja.AssertFunction(socket.Get("onclose")); ok {
		event := h.vm.NewObject()
		event.Set("code", 1006)
		if _, err := onClose(socket, event); err != nil {
			h.t.Fatalf("served socket onclose: %v", err)
		}
	}
}

func (h *servedPageHarness) runTimers() {
	h.t.Helper()
	for len(h.timers) > 0 {
		fn := h.timers[0]
		h.timers = h.timers[1:]
		if _, err := fn(h.vm.GlobalObject()); err != nil {
			h.t.Fatalf("served timer: %v", err)
		}
	}
}

func (h *servedPageHarness) emitText(socket *goja.Object, frame any) {
	h.t.Helper()
	b, err := json.Marshal(frame)
	if err != nil {
		h.t.Fatal(err)
	}
	h.emitRaw(socket, string(b))
}

func (h *servedPageHarness) emitRaw(socket *goja.Object, raw string) {
	h.t.Helper()
	onMessage, ok := goja.AssertFunction(socket.Get("onmessage"))
	if !ok {
		h.t.Fatal("served socket onmessage is not callable")
	}
	event := h.vm.NewObject()
	event.Set("data", raw)
	if _, err := onMessage(socket, event); err != nil {
		h.t.Fatalf("served onmessage: %v", err)
	}
}

func (h *servedPageHarness) hello(connection string, generation int64, caps []string) {
	h.emitText(h.socket, map[string]any{
		"type": "hello", "connectionId": connection, "sessionId": "controlled_pty:goja",
		"generation": generation, "capabilities": caps,
	})
}

func (h *servedPageHarness) pageInput(text, operation, part string) {
	h.t.Helper()
	fn, ok := goja.AssertFunction(h.vm.Get("pokitSendInput"))
	if !ok {
		h.t.Fatal("served pokitSendInput is not callable")
	}
	if _, err := fn(h.vm.GlobalObject(), h.vm.ToValue(text), h.vm.ToValue(operation), h.vm.ToValue(part)); err != nil {
		h.t.Fatalf("served pokitSendInput: %v", err)
	}
}

func (h *servedPageHarness) keyboard(text string) {
	h.t.Helper()
	if _, err := h.onData(h.vm.GlobalObject(), h.vm.ToValue(text)); err != nil {
		h.t.Fatalf("served direct keyboard: %v", err)
	}
}

func terminalInputFrames(t *testing.T, sent []string) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for _, raw := range sent {
		var frame map[string]any
		if err := json.Unmarshal([]byte(raw), &frame); err != nil {
			continue // the served page may emit non-input text controls.
		}
		if frame["type"] == "terminal_input" {
			frames = append(frames, frame)
		}
	}
	return frames
}

func decodedInput(t *testing.T, frame map[string]any) string {
	t.Helper()
	payload, ok := frame["payload"].(string)
	if !ok {
		t.Fatalf("payload=%#v", frame["payload"])
	}
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func countType(posts []map[string]any, typ string) int {
	n := 0
	for _, post := range posts {
		if post["type"] == typ {
			n++
		}
	}
	return n
}

// c1NativeDelivery is a deliberately narrow native-message consumer matching
// FeedScreen's Input-B delivery rule: a pending frame is identity-bound, a
// changed hello makes unresolved delivery unknown, and a line is Delivered
// only after both its text and Enter results are accepted. It consumes only
// messages emitted by the actual served page's ReactNativeWebView.postMessage.
type c1NativeDelivery struct {
	connectionID string
	sessionID    string
	generation   float64
	pending      map[string]c1NativePending
	lines        map[string]map[string]string
	status       string
}

type c1NativePending struct {
	operation string
	part      string
}

func newC1NativeDelivery() *c1NativeDelivery {
	return &c1NativeDelivery{pending: make(map[string]c1NativePending), lines: make(map[string]map[string]string)}
}

func (n *c1NativeDelivery) consume(frame map[string]any) {
	typ, _ := frame["type"].(string)
	connectionID, _ := frame["connectionId"].(string)
	sessionID, _ := frame["sessionId"].(string)
	generation, _ := frame["generation"].(float64)
	sameIdentity := connectionID == n.connectionID && sessionID == n.sessionID && generation == n.generation
	switch typ {
	case "hello":
		if n.connectionID != "" && !sameIdentity && len(n.pending) > 0 {
			n.status = "Possible partial delivery"
			n.pending = make(map[string]c1NativePending)
			n.lines = make(map[string]map[string]string)
		}
		n.connectionID, n.sessionID, n.generation = connectionID, sessionID, generation
	case "input_pending":
		if !sameIdentity {
			return
		}
		inputID, _ := frame["inputId"].(string)
		if inputID == "" {
			return
		}
		operation, _ := frame["operationId"].(string)
		part, _ := frame["part"].(string)
		n.pending[inputID] = c1NativePending{operation: operation, part: part}
		if operation != "" && (part == "text" || part == "enter") && n.lines[operation] == nil {
			n.lines[operation] = make(map[string]string)
		}
		n.status = "Sent to socket"
	case "input_result":
		inputID, _ := frame["inputId"].(string)
		outcome, _ := frame["outcome"].(string)
		pending, ok := n.pending[inputID]
		if !sameIdentity || !ok {
			return
		}
		delete(n.pending, inputID)
		if pending.operation == "" || (pending.part != "text" && pending.part != "enter") {
			if outcome == "accepted" {
				n.status = "Delivered to terminal"
			}
			return
		}
		line := n.lines[pending.operation]
		line[pending.part] = outcome
		if line["text"] == "accepted" && line["enter"] == "accepted" {
			n.status = "Delivered to terminal"
		}
	}
}

// This is the TERM-C1 production chain: literal HandleHTML script running in
// Goja → its WebSocket.send → real gorilla HandleWS → real server ACK → that
// same page's onmessage → ReactNativeWebView.postMessage → native delivery
// receiver. No Go helper constructs terminal_input frames or fabricates ACKs.
func TestTERM_C1_ServedPageGojaRealWebSocketToNativeDelivery(t *testing.T) {
	f := newC1Fixture(t)
	h := newLiveServedPageHarness(t, f)
	h.pumpText(h.socket) // real HandleWS hello
	if got := countType(h.posts, "hello"); got != 1 {
		t.Fatalf("native hello deliveries=%d, want 1", got)
	}

	beforeCalls := f.writes.Calls()
	// A real two-frame line proves the native receiver does not claim delivery
	// after only the text ACK.
	h.pageInput("two-frame", "line-real", "text")
	h.pumpText(h.socket)
	if h.delivery.status == "Delivered to terminal" {
		t.Fatal("native delivery claimed Delivered after only the text ACK")
	}
	h.pageInput("\r", "line-real", "enter")
	h.pumpText(h.socket)
	if h.delivery.status != "Delivered to terminal" {
		t.Fatalf("two accepted real ACKs status=%q, want Delivered to terminal", h.delivery.status)
	}

	// Every surface now executes the page's actual sender and is acknowledged
	// by HandleWS. Macros are intentionally here (not just in a fake WS test).
	paste := "paste\nbody"
	h.pageInput(paste, "paste-real", "text")
	h.pumpText(h.socket)
	// Ctrl+C is last because it intentionally terminates this test PTY; it has
	// still travelled through the same live page → HandleWS chain.
	macros := [][]byte{{27}, {9}, {27, 91, 65}, {27, 91, 66}, {27, 91, 68}, {27, 91, 67}, {121, 13}, {110, 13}, {13}, {3}}
	for i, macro := range macros {
		h.pageInput(string(macro), "macro-real-"+string(rune('a'+i)), "text")
		h.pumpText(h.socket)
	}

	frames := terminalInputFrames(t, h.sent)
	if got, want := len(frames), 13; got != want {
		t.Fatalf("real served-page terminal_input count=%d, want %d", got, want)
	}
	wantPayloads := append([]string{"two-frame", "\r", paste}, byteSlicesToStrings(macros)...)
	for i, want := range wantPayloads {
		if got := decodedInput(t, frames[i]); got != want {
			t.Fatalf("real served-page input %d=%q, want %q", i, got, want)
		}
	}
	if got, want := f.writes.Calls(), beforeCalls+len(wantPayloads); got != want {
		t.Fatalf("HandleWS WriteInput calls=%d, want %d", got, want)
	}
	if got, want := countType(h.posts, "input_result"), len(wantPayloads); got != want {
		t.Fatalf("native input_result deliveries=%d, want %d", got, want)
	}
}

func TestTERM_C1_ServedPageGojaRealDirectCtrlC(t *testing.T) {
	f := newC1Fixture(t)
	h := newLiveServedPageHarness(t, f)
	h.pumpText(h.socket)
	beforeCalls := f.writes.Calls()
	h.keyboard("\x03") // actual term.onData → actual pokitSendInput → live HandleWS
	h.pumpText(h.socket)
	frames := terminalInputFrames(t, h.sent)
	if len(frames) != 1 || decodedInput(t, frames[0]) != "\x03" {
		t.Fatalf("direct keyboard Ctrl+C frames=%#v", frames)
	}
	if got, want := f.writes.Calls(), beforeCalls+1; got != want {
		t.Fatalf("direct Ctrl+C HandleWS WriteInput calls=%d, want %d", got, want)
	}
	if h.delivery.status != "Delivered to terminal" {
		t.Fatalf("direct Ctrl+C native status=%q, want Delivered to terminal", h.delivery.status)
	}
}

func TestTERM_C1_ServedPageGojaRealEveryMacroDeniedZeroPTYWrite(t *testing.T) {
	f := newC1Fixture(t)
	// Issue the live WS ticket as a viewer. The actual HandleWS hello omits
	// terminal:input, so the served bridge must deny every macro before send.
	f.principal.Permissions = []string{devicetrust.PermSessionsRead}
	h := newLiveServedPageHarness(t, f)
	h.pumpText(h.socket)
	beforeCalls := f.writes.Calls()
	macros := [][]byte{{3}, {27}, {9}, {27, 91, 65}, {27, 91, 66}, {27, 91, 68}, {27, 91, 67}, {121, 13}, {110, 13}, {13}}
	for i, macro := range macros {
		h.pageInput(string(macro), "denied-macro-"+string(rune('a'+i)), "text")
	}
	if got := len(terminalInputFrames(t, h.sent)); got != 0 {
		t.Fatalf("viewer macros emitted %d terminal_input frames", got)
	}
	if got, want := f.writes.Calls(), beforeCalls; got != want {
		t.Fatalf("viewer macro HandleWS WriteInput calls=%d, want %d", got, want)
	}
	if got, want := countType(h.posts, "delivery_unknown"), len(macros); got != want {
		t.Fatalf("viewer macro native denials=%d, want %d", got, want)
	}
}

func byteSlicesToStrings(in [][]byte) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = string(in[i])
	}
	return out
}

func TestTERM_C1_ServedPageGojaPendingDuplicateAndReconnect(t *testing.T) {
	f := newC1Fixture(t)
	h := newLiveServedPageHarness(t, f)
	realHello := h.pumpText(h.socket)

	// Create an actual pending HandleWS input, then replay the literal server
	// hello before its real ACK is pumped. The page must suppress the duplicate
	// and FeedScreen-like native state must retain the pending operation.
	h.pageInput("duplicate-pending", "", "text")
	if h.delivery.status != "Sent to socket" || len(h.delivery.pending) != 1 {
		t.Fatalf("expected one pending native delivery, status=%q pending=%d", h.delivery.status, len(h.delivery.pending))
	}
	h.emitRaw(h.socket, realHello)
	if got := countType(h.posts, "hello"); got != 1 {
		t.Fatalf("duplicate real hello forwarded %d times, want 1", got)
	}
	if len(h.delivery.pending) != 1 {
		t.Fatal("duplicate hello cleared native pending input")
	}
	h.pumpText(h.socket) // actual HandleWS accepted ACK
	if h.delivery.status != "Delivered to terminal" {
		t.Fatalf("duplicate hello prevented real ACK delivery: %q", h.delivery.status)
	}

	// Leave this server ACK unread by JavaScript, then close the actual gorilla
	// peer. The page's real onclose/reconnect path creates a new socket; its
	// actual new HandleWS hello turns the unresolved native operation into
	// Possible partial delivery.
	beforeUnacknowledgedWrite := f.writes.Calls()
	resultsBeforeClose := countType(h.posts, "input_result")
	h.pageInput("unacknowledged-before-close", "", "text")
	if len(h.delivery.pending) != 1 {
		t.Fatalf("expected unacknowledged pending input, got %d", len(h.delivery.pending))
	}
	waitForC1WriteCalls(t, f.writes, beforeUnacknowledgedWrite+1)
	if got := countType(h.posts, "input_result"); got != resultsBeforeClose {
		t.Fatalf("ACK was consumed before real socket close: results=%d, want %d", got, resultsBeforeClose)
	}
	old := h.socket
	h.closeLiveSocket(old)
	h.runTimers()
	if h.socket == old || h.oldSock != old {
		t.Fatal("served onclose reconnect did not replace the real WebSocket")
	}
	h.pumpText(h.socket) // new real HandleWS hello
	if h.delivery.status != "Possible partial delivery" {
		t.Fatalf("reconnect with unread real ACK status=%q, want Possible partial delivery", h.delivery.status)
	}
	if len(h.delivery.pending) != 0 {
		t.Fatal("new real hello did not retire unresolved native pending input")
	}
}

func waitForC1WriteCalls(t *testing.T, writes *c1WriteCounter, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if writes.Calls() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("HandleWS WriteInput calls=%d, want at least %d", writes.Calls(), want)
}

func TestTERM_C1_ServedPageGojaInputSurfacesAndAcceptedResults(t *testing.T) {
	h := newServedPageHarness(t)
	h.hello("conn-1", 7, []string{"terminal:input"})
	h.keyboard("\x03") // actual term.onData → actual pokitSendInput
	h.pageInput("paste\nbody", "paste-1", "text")
	macros := [][]byte{{3}, {27}, {9}, {27, 91, 65}, {27, 91, 66}, {27, 91, 68}, {27, 91, 67}, {121, 13}, {110, 13}, {13}}
	for i, macro := range macros {
		h.pageInput(string(macro), "macro-"+string(rune('a'+i)), "text")
	}
	h.pageInput("two frame", "line-1", "text")
	h.pageInput("\r", "line-1", "enter")

	frames := terminalInputFrames(t, h.sent)
	if len(frames) != 14 {
		t.Fatalf("terminal_input count=%d, sent=%v", len(frames), h.sent)
	}
	if got := decodedInput(t, frames[0]); got != "\x03" {
		t.Fatalf("direct Ctrl+C=%q, want 0x03", got)
	}
	if got := decodedInput(t, frames[1]); got != "paste\nbody" {
		t.Fatalf("paste=%q", got)
	}
	for i, macro := range macros {
		if got := decodedInput(t, frames[i+2]); got != string(macro) {
			t.Fatalf("macro %d=%q want=%q", i, got, string(macro))
		}
	}
	if got, want := decodedInput(t, frames[12]), "two frame"; got != want {
		t.Fatalf("line text=%q want=%q", got, want)
	}
	if got := decodedInput(t, frames[13]); got != "\r" {
		t.Fatalf("line enter=%q", got)
	}
	if got := countType(h.posts, "input_pending"); got != 14 {
		t.Fatalf("input_pending=%d, want 14", got)
	}

	for _, frame := range frames {
		h.emitText(h.socket, map[string]any{
			"type": "input_result", "connectionId": "conn-1", "sessionId": "controlled_pty:goja",
			"generation": 7, "inputId": frame["inputId"], "outcome": "accepted",
		})
	}
	if got := countType(h.posts, "input_result"); got != 14 {
		t.Fatalf("accepted input_result=%d, want 14", got)
	}
}

func TestTERM_C1_ServedPageGojaFailsClosedAndReconnects(t *testing.T) {
	denied := []map[string]any{
		{"connectionId": "conn-1", "sessionId": "wrong", "generation": 7, "capabilities": []string{"terminal:input"}},
		{"connectionId": "", "sessionId": "controlled_pty:goja", "generation": 7, "capabilities": []string{"terminal:input"}},
		{"connectionId": "conn-1", "sessionId": "controlled_pty:goja", "generation": 7.5, "capabilities": []string{"terminal:input"}},
		{"connectionId": "conn-1", "sessionId": "controlled_pty:goja", "generation": 7}, // missing capabilities
		{"connectionId": "conn-1", "sessionId": "controlled_pty:goja", "generation": 7, "capabilities": []string{"terminal:unknown"}},
	}
	for _, hello := range denied {
		h := newServedPageHarness(t)
		hello["type"] = "hello"
		h.emitText(h.socket, hello)
		h.keyboard("\x03")
		h.pageInput("paste", "paste-1", "text")
		h.pageInput("\r", "line-1", "enter")
		if got := len(terminalInputFrames(t, h.sent)); got != 0 {
			t.Fatalf("denied hello %#v wrote %d terminal frames", hello, got)
		}
		if len(h.writes) != 0 {
			t.Fatalf("denied hello %#v wrote terminal output %q", hello, h.writes)
		}
	}
	malformed := newServedPageHarness(t)
	malformed.emitRaw(malformed.socket, `{"type":"hello"`)
	malformed.keyboard("\x03")
	if got := len(terminalInputFrames(t, malformed.sent)); got != 0 {
		t.Fatalf("malformed control wrote %d terminal frames", got)
	}

	h := newServedPageHarness(t)
	h.hello("conn-1", 7, []string{"terminal:input"})
	h.hello("conn-1", 7, []string{"terminal:input"}) // duplicate must be inert
	if got := countType(h.posts, "hello"); got != 1 {
		t.Fatalf("duplicate hello deliveries=%d, want 1", got)
	}
	postsBeforeMismatchedResults := len(h.posts)
	for _, result := range []map[string]any{
		{"type": "input_result", "connectionId": "wrong", "sessionId": "controlled_pty:goja", "generation": 7, "inputId": "x", "outcome": "accepted"},
		{"type": "input_result", "connectionId": "conn-1", "sessionId": "wrong", "generation": 7, "inputId": "x", "outcome": "accepted"},
		{"type": "input_result", "connectionId": "conn-1", "sessionId": "controlled_pty:goja", "generation": 8, "inputId": "x", "outcome": "accepted"},
	} {
		h.emitText(h.socket, result)
	}
	if len(h.posts) != postsBeforeMismatchedResults {
		t.Fatal("wrong input-result identity reached the WebView bridge")
	}
	postsBefore := len(h.posts)
	// A same-socket identity change revokes both page and native bridge state.
	h.hello("conn-2", 8, []string{"terminal:input"})
	h.pageInput("must not write", "line-2", "text")
	if got := len(terminalInputFrames(t, h.sent)); got != 0 {
		t.Fatalf("same-socket rebind wrote %d terminal frames", got)
	}
	if len(h.posts) != postsBefore+2 || h.posts[postsBefore]["type"] != "read_only" || h.posts[postsBefore+1]["type"] != "delivery_unknown" {
		t.Fatalf("rebind posts=%#v", h.posts[postsBefore:])
	}

	// The actual page reconnect creates a replacement socket. Only that socket
	// may establish the new identity; a stale socket cannot mutate the bridge.
	old := h.socket
	fn, ok := goja.AssertFunction(h.vm.Get("connect"))
	if !ok {
		t.Fatal("served connect is not callable")
	}
	if _, err := fn(h.vm.GlobalObject()); err != nil {
		t.Fatal(err)
	}
	if h.socket == old || h.oldSock != old {
		t.Fatal("served reconnect did not replace WebSocket")
	}
	h.hello("conn-3", 9, []string{"terminal:input"})
	postsAfterReconnect := len(h.posts)
	h.emitText(old, map[string]any{"type": "read_only", "reason": "stale"})
	if len(h.posts) != postsAfterReconnect {
		t.Fatal("stale socket forwarded a control frame")
	}
	h.pageInput("new socket", "line-3", "text")
	if got := len(terminalInputFrames(t, h.sent)); got != 1 {
		t.Fatalf("replacement socket terminal frames=%d", got)
	}
}
