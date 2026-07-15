package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// ── SP1-P1: production composition proof ──
//
// The REAL composition root (NewAppWithDeps) wires the approval store into
// the managed service (app.go SetApprovalStore); this test drives a certified
// provider approval request through the production pump and reads it back as
// the bounded NON-ACTIONABLE safe DTO on the authenticated GET /api/sessions
// route — the same path the mobile client consumes. No test code touches
// SetApprovalStore or the store directly.

const (
	sp1Thread    = "thread-S1"
	sp1Turn      = "turn-S1"
	sp1SecretCmd = "SP1-SECRET-CMD-51c2"
	sp1SecretCWD = "/Users/sp1-secret/workdir"
)

// sp1ApprovalRaw is the evidence-exact certified request emitted by the fake
// provider (CP0 wire shape; top-level integer JSON-RPC id).
const sp1ApprovalRaw = `{"jsonrpc":"2.0","id":7,"method":"item/commandExecution/requestApproval",` +
	`"params":{"threadId":"` + sp1Thread + `","turnId":"` + sp1Turn + `","itemId":"item-4",` +
	`"command":["` + sp1SecretCmd + `"],"cwd":"` + sp1SecretCWD + `","environmentId":"local",` +
	`"availableDecisions":["accept",{"acceptWithExecpolicyAmendment":{}},"cancel"]}}`

// sp1FakeLauncher answers the exact handshake and, on turn/start, emits the
// turn binding followed by the certified approval request raw line.
type sp1FakeLauncher struct {
	mu    sync.Mutex
	procs int
}

type sp1FakeProc struct {
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	killed  chan struct{}
	once    sync.Once
}

func (p *sp1FakeProc) Stdin() io.Writer  { return p.stdinW }
func (p *sp1FakeProc) Stdout() io.Reader { return p.stdoutR }
func (p *sp1FakeProc) Term() error       { return p.Kill() }
func (p *sp1FakeProc) Kill() error {
	p.once.Do(func() {
		close(p.killed)
		p.stdinR.CloseWithError(io.ErrClosedPipe)
		p.stdoutW.CloseWithError(io.EOF)
	})
	return nil
}
func (p *sp1FakeProc) Wait() error {
	<-p.killed
	return nil
}
func (p *sp1FakeProc) OpaqueID() string { return "sp1-fake-proc" }

func (l *sp1FakeLauncher) Launch(_ string, _ []string) (term.ManagedProcess, error) {
	l.mu.Lock()
	l.procs++
	l.mu.Unlock()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	p := &sp1FakeProc{stdinR: stdinR, stdinW: stdinW, stdoutR: stdoutR, stdoutW: stdoutW, killed: make(chan struct{})}
	go func() {
		sc := bufio.NewScanner(stdinR)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		out := func(m map[string]any) {
			b, _ := json.Marshal(m)
			stdoutW.Write(append(b, '\n'))
		}
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(bytes.TrimSpace(sc.Bytes()), &m) != nil {
				continue
			}
			id, _ := m["id"].(float64)
			switch m["method"] {
			case "initialize":
				out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
			case "thread/start":
				out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"threadId": sp1Thread}})
			case "turn/start":
				out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"turn": map[string]any{"id": sp1Turn}}})
				out(map[string]any{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{"threadId": sp1Thread, "turn": map[string]any{"id": sp1Turn}}})
				// The certified approval request, byte-exact raw line.
				stdoutW.Write([]byte(sp1ApprovalRaw + "\n"))
			}
		}
	}()
	return p, nil
}

// getAll performs an authenticated GET and returns the FULL body.
func (f *remoteFixture) getAll(t *testing.T, path, bearer string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, f.srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, body
}

// TestSP1P1_CompositionRequestToSafeDTO: certified provider request →
// production-wired ApprovalStore → authenticated GET /api/sessions managed
// row carrying exactly one safe approval DTO with zero provider bytes.
// SP1-P2B: the production composition root now performs the atomic
// InstallApprovalExecution transition, so the record is ACTIONABLE with
// exactly the two certified options (allow_once/approve, deny/reject) and
// Pokit-owned labels — still no payload, command, cwd, or schema material.
func TestSP1P1_CompositionRequestToSafeDTO(t *testing.T) {
	fl := &sp1FakeLauncher{}
	f := newRemoteFixtureWith(t, nil, nil, func(cfg *Config, deps *Dependencies) {
		cfg.EnableManagedCodex = true
		deps.Managed = term.NewManagedCodexServiceForTest(fl, func() error { return nil })
	})
	ownerPriv, ownerID := f.pairDevice(t, "owner-phone")
	ownerToken := f.token(t, ownerID, ownerPriv)

	sessionID, err := f.app.managed.CreateAttached("")
	if err != nil {
		t.Fatalf("managed create: %v", err)
	}
	// Start the turn over the REAL authenticated prompt route; the fake
	// provider then emits the certified approval request.
	code, body := f.doJSON(t, "POST", "/api/managed-sessions/"+sessionID+"/prompt", ownerToken, `{"epoch":1,"text":"build it"}`)
	if code != http.StatusOK {
		t.Fatalf("prompt: code=%d body=%s", code, body)
	}

	type optionRow struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Kind  string `json:"kind"`
	}
	type sessionRow struct {
		ID        string `json:"id"`
		Approvals []struct {
			ID         string      `json:"id"`
			SessionID  string      `json:"sessionId"`
			State      string      `json:"state"`
			Actionable bool        `json:"actionable"`
			Options    []optionRow `json:"options"`
		} `json:"approvals"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, raw := f.getAll(t, "/api/sessions", ownerToken)
		if code != http.StatusOK {
			t.Fatalf("GET /api/sessions code=%d body=%s", code, raw)
		}
		var rows []sessionRow
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatalf("decode sessions: %v (%s)", err, raw)
		}
		for _, row := range rows {
			if row.ID != sessionID || len(row.Approvals) == 0 {
				continue
			}
			// The production-composed safe DTO: actionable (P2B activation)
			// with EXACTLY the two certified options and Pokit-owned labels.
			a := row.Approvals[0]
			if len(row.Approvals) != 1 || !a.Actionable || len(a.Options) != 2 ||
				a.Options[0].ID != "allow_once" || a.Options[0].Kind != "approve" || a.Options[0].Label != "Approve" ||
				a.Options[1].ID != "deny" || a.Options[1].Kind != "reject" || a.Options[1].Label != "Reject" {
				t.Fatalf("managed row approval must be single actionable with the certified options: %+v", row.Approvals)
			}
			if a.ID != "codexas-1-7" || a.SessionID != sessionID || a.State != "pending" {
				t.Fatalf("unexpected safe DTO identity: %+v", a)
			}
			// Provider bytes never reach the authenticated read surface.
			for _, secret := range []string{sp1SecretCmd, sp1SecretCWD, "availableDecisions", "requestApproval", "decision", "jsonrpc"} {
				if strings.Contains(string(raw), secret) {
					t.Fatalf("provider material %q leaked into /api/sessions", secret)
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("approval never appeared on /api/sessions for %s", sessionID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSP2B_InstallFailureFallsBackToObservation: when the atomic activation
// transition fails (a runtime already existed before NewAppWithDeps could
// install), the composition root keeps BOTH capabilities off — the certified
// request yields a NON-actionable zero-option record and the authenticated
// action POST fails closed with zero provider response writes.
func TestSP2B_InstallFailureFallsBackToObservation(t *testing.T) {
	fl := &sp1FakeLauncher{}
	f := newRemoteFixtureWith(t, nil, nil, func(cfg *Config, deps *Dependencies) {
		cfg.EnableManagedCodex = true
		svc := term.NewManagedCodexServiceForTest(fl, func() error { return nil })
		// A runtime created BEFORE the app's activation transition forces
		// InstallApprovalExecution to fail (activation must precede the
		// first epoch).
		if _, err := svc.CreateAttached(""); err != nil {
			t.Fatalf("pre-create: %v", err)
		}
		deps.Managed = svc
	})
	ownerPriv, ownerID := f.pairDevice(t, "owner-phone")
	ownerToken := f.token(t, ownerID, ownerPriv)

	// A session created AFTER the (failed) activation: it has the P1
	// observation sink but NO actionable capability.
	sessionID, err := f.app.managed.CreateAttached("")
	if err != nil {
		t.Fatalf("managed create: %v", err)
	}
	code, body := f.doJSON(t, "POST", "/api/managed-sessions/"+sessionID+"/prompt", ownerToken, `{"epoch":2,"text":"build it"}`)
	if code != http.StatusOK {
		t.Fatalf("prompt: code=%d body=%s", code, body)
	}
	type sessionRow struct {
		ID        string `json:"id"`
		Approvals []struct {
			ID         string `json:"id"`
			Actionable bool   `json:"actionable"`
			Options    []any  `json:"options"`
		} `json:"approvals"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, raw := f.getAll(t, "/api/sessions", ownerToken)
		if code != http.StatusOK {
			t.Fatalf("GET /api/sessions code=%d", code)
		}
		var rows []sessionRow
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatalf("decode sessions: %v", err)
		}
		found := false
		for _, row := range rows {
			if row.ID != sessionID || len(row.Approvals) == 0 {
				continue
			}
			found = true
			a := row.Approvals[0]
			if a.Actionable || len(a.Options) != 0 {
				t.Fatalf("failed install must leave the record non-actionable: %+v", row.Approvals)
			}
			// The authenticated action POST fails closed (409 not_actionable).
			code, body := f.doJSON(t, "POST", "/api/sessions/"+sessionID+"/approvals/"+a.ID, ownerToken, `{"action":"allow_once","idempotencyKey":"k1"}`)
			if code != http.StatusConflict {
				t.Fatalf("action on non-actionable record: code=%d body=%s", code, body)
			}
		}
		if found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("approval never appeared for %s", sessionID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
