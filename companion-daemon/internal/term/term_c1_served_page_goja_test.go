package term

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/dop251/goja"
)

// servedPageHarness executes the literal HTML returned by HandleHTML in Goja.
// Browser primitives are minimal host boundaries only: Terminal records output,
// WebSocket records browser->daemon frames, and ReactNativeWebView records the
// actual page bridge's postMessage payloads. The control bridge itself is never
// reproduced in Go.
type servedPageHarness struct {
	t       *testing.T
	vm      *goja.Runtime
	socket  *goja.Object
	oldSock *goja.Object
	onData  goja.Callable
	sent    []string
	posts   []map[string]any
	writes  []string
}

func newServedPageHarness(t *testing.T) *servedPageHarness {
	t.Helper()
	rr := httptest.NewRecorder()
	(&Handlers{}).HandleHTML(rr, httptest.NewRequest("GET", "http://daemon.test/term/?session=controlled_pty:goja", nil))
	if rr.Code != 200 {
		t.Fatalf("HandleHTML status=%d", rr.Code)
	}
	matches := regexp.MustCompile(`(?s)<script>\s*(.*?)</script>\s*</body>`).FindStringSubmatch(rr.Body.String())
	if len(matches) != 2 {
		t.Fatal("served inline terminal script not found")
	}

	h := &servedPageHarness{t: t, vm: goja.New()}
	vm := h.vm
	window := vm.GlobalObject()
	if err := vm.Set("window", window); err != nil {
		t.Fatal(err)
	}
	location := vm.NewObject()
	location.Set("protocol", "http:")
	location.Set("host", "daemon.test")
	location.Set("search", "?session=controlled_pty:goja")
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
	window.Set("setTimeout", func(goja.FunctionCall) goja.Value { return vm.ToValue(1) })
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
		socket.Set("send", func(call goja.FunctionCall) goja.Value {
			h.sent = append(h.sent, call.Argument(0).String())
			return goja.Undefined()
		})
		socket.Set("close", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
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
