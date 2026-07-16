package main

import (
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/term"
)

type compFakeLauncher struct {
	mu        sync.Mutex
	allArgs   [][]string
	procs     []*compPipeProc
	resumeW   *io.PipeWriter
	resumeWCh chan struct{}
}

type compPipeProc struct {
	StdinW  *io.PipeWriter
	StdoutR *io.PipeReader
	StdoutW *io.PipeWriter
	killCh  chan struct{}
	once    sync.Once
}

func (p *compPipeProc) Stdin() io.Writer  { return p.StdinW }
func (p *compPipeProc) Stdout() io.Reader { return p.StdoutR }
func (p *compPipeProc) Term() error       { return p.Kill() }
func (p *compPipeProc) Kill() error {
	p.once.Do(func() { close(p.killCh); p.StdinW.Close(); p.StdoutW.Close() })
	return nil
}
func (p *compPipeProc) Wait() error      { <-p.killCh; return nil }
func (p *compPipeProc) PID() int         { return 0 }
func (p *compPipeProc) OpaqueID() string { return "comp-fake" }

func (l *compFakeLauncher) Launch(_ string, argv []string) (term.ManagedProcess, error) {
	l.mu.Lock()
	l.allArgs = append(l.allArgs, append([]string(nil), argv...))
	isResume := len(l.allArgs) >= 2
	l.mu.Unlock()
	_, sw := io.Pipe()
	pr, pw := io.Pipe()
	p := &compPipeProc{StdinW: sw, StdoutR: pr, StdoutW: pw, killCh: make(chan struct{})}
	l.mu.Lock()
	l.procs = append(l.procs, p)
	if isResume && l.resumeWCh != nil {
		l.resumeW = pw
		close(l.resumeWCh)
	}
	l.mu.Unlock()
	return p, nil
}

type compFakeAttestor struct{}

func (a *compFakeAttestor) Certify(_ string) error { return nil }

func newProviderSim(t *testing.T) (*compFakeLauncher, *term.ManagedClaudeService, *term.AuthoritativeApprovalStore) {
	t.Helper()
	launcher := &compFakeLauncher{}
	cfg := term.ClaudeEntryConfig{
		Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209",
		PinnedPath:   "/tmp/fake-claude",
		PinnedDigest: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	svc := term.NewManagedClaudeService(cfg, launcher, &compFakeAttestor{})
	store := term.NewApprovalStore()
	svc.SetApprovalStore(store)
	launcher.resumeWCh = make(chan struct{})
	return launcher, svc, store
}

func readHookURL(t *testing.T, path string) string {
	t.Helper()
	data, _ := os.ReadFile(path)
	s := strings.TrimSpace(string(data))
	start := strings.IndexByte(s, '\'')
	end := strings.LastIndexByte(s, '\'')
	if start < 0 || end <= start {
		t.Fatalf("cannot parse hook script: %s", s)
	}
	return s[start+1 : end]
}

func captureBridgeURLs(t *testing.T, launcher *compFakeLauncher) (resumeURL, posttoolURL string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		time.Sleep(10 * time.Millisecond)
		launcher.mu.Lock()
		if len(launcher.allArgs) >= 2 {
			args := launcher.allArgs[1]
			for j, a := range args {
				if a == "--settings" && j+1 < len(args) {
					hd := strings.TrimSuffix(args[j+1], "/settings.json")
					resumeURL = readHookURL(t, hd+"/hook_resume.sh")
					posttoolURL = readHookURL(t, hd+"/hook_posttool.sh")
					launcher.mu.Unlock()
					return
				}
			}
		}
		launcher.mu.Unlock()
	}
	t.Fatal("resume launch never observed")
	return
}

func fireResumeHook(t *testing.T, url, sid, tuid, tn, inputJSON string) []byte {
	t.Helper()
	body := `{"session_id":"` + sid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PreToolUse"}`
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("resume hook: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b
}

func firePostToolHook(t *testing.T, url, sid, tuid, tn, inputJSON string) {
	t.Helper()
	body := `{"session_id":"` + sid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PostToolUse"}`
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("posttool hook: %v", err)
	}
	resp.Body.Close()
}

func makeSetup(t *testing.T, optionID string) (*compFakeLauncher, *term.ManagedClaudeService, *term.AuthoritativeApprovalStore, term.ClaimResult, term.RuntimeRef, string, string, string) {
	t.Helper()
	launcher, svc, store := newProviderSim(t)
	sid, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	rt := term.RuntimeRef{Adapter: "claude_headless", Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	aid := "claude-comp-" + optionID
	csid := "claude-sess-" + optionID
	tuid := "call_comp_" + optionID
	tn := "Bash"
	dgst := term.CanonicalDigest([]byte(`{"command":"echo hello"}`))
	svc.Coordinator().ReserveIdentity(aid, csid, tuid, tn, dgst, sid, rt)

	ab := term.ClaudeHookResponseBytes("allow")
	db := term.ClaudeHookResponseBytes("deny")
	store.IngestObserved(term.ApprovalIngest{SessionID: sid, LaunchGen: 1, StreamGen: 0, Provider: "claude_headless", Version: "2.1.209", Items: []term.ApprovalIngestItem{{
		Approval: agent.AgentApproval{
			ID: aid, SessionID: sid, AgentKind: "claude_headless",
			Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
			Options: []agent.InteractionOption{
				{ID: "allow_once", Label: "Allow once", Kind: "approve"},
				{ID: "deny", Label: "Deny", Kind: "reject"},
			},
		},
		Provenance: contract.ProvenanceProviderHook, Actionable: true,
		DeliveryMaterial: []term.ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: ab},
			{OptionID: "deny", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: db},
		},
	}}})
	reqCtx := term.RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "b2", Permissions: []string{"x"}}
	claim := store.ClaimForExecution(term.ClaimRequest{SessionID: sid, ApprovalID: aid, OptionID: optionID, Input: "", Runtime: rt, Requester: reqCtx, IdempotencyKey: "test.comp." + optionID})
	if claim.Outcome != term.ClaimGranted {
		t.Fatalf("ClaimForExecution: %s", claim.Outcome)
	}
	return launcher, svc, store, claim, rt, csid, tuid, tn
}

func TestClaudeDelivery_CompositionAllowAccepted(t *testing.T) {
	l, svc, store, claim, _, csid, tuid, tn := makeSetup(t, "allow_once")
	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(5 * time.Second)
	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()
	resumeURL, posttoolURL := captureBridgeURLs(t, l)
	r := fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo hello"}`)
	if !bytesEq(r, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("resume response: %s", r)
	}
	firePostToolHook(t, posttoolURL, csid, tuid, tn, `{"command":"echo hello"}`)
	wg.Wait()
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("expected accepted, got %s", receipt.Outcome)
	}
	if !store.RecordDelivery(receipt).Committed {
		t.Fatal("not committed")
	}
}

func TestClaudeDelivery_CompositionDenyAccepted(t *testing.T) {
	l, svc, store, claim, _, csid, tuid, tn := makeSetup(t, "deny")
	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(5 * time.Second)
	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()
	resumeURL, _ := captureBridgeURLs(t, l)
	r := fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo hello"}`)
	if string(r) != string(term.ClaudeHookResponseBytes("deny")) {
		t.Fatalf("resume response: %s", r)
	}
	// R6-B: the denial event must carry the evidence-shape entry {tool_use_id,
	// tool_name, tool_input} with raw tool_input. The digest recomputed from
	// this tool_input must equal the digest stored at observation.
	denialJSON := `{"type":"result","session_id":"` + csid + `","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"command":"echo hello"}}]}` + "\n"
	// Wait for resume writer via channel (race-free).
	select {
	case <-l.resumeWCh:
	case <-time.After(time.Second):
		t.Fatal("resume writer not ready")
	}
	l.resumeW.Write([]byte(denialJSON))
	time.Sleep(100 * time.Millisecond)
	wg.Wait()
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("expected accepted for deny, got %s", receipt.Outcome)
	}
	if !store.RecordDelivery(receipt).Committed {
		t.Fatal("not committed")
	}
}

func TestClaudeDelivery_DenyPostToolUseCannotCommit(t *testing.T) {
	l, svc, store, claim, _, csid, tuid, tn := makeSetup(t, "deny")
	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(2 * time.Second)
	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()
	resumeURL, posttoolURL := captureBridgeURLs(t, l)
	fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo hello"}`)
	firePostToolHook(t, posttoolURL, csid, tuid, tn, `{"command":"echo hello"}`)
	wg.Wait()
	if receipt.Outcome == term.DeliveryAccepted {
		t.Fatal("PostToolUse after deny must not succeed")
	}
	if store.RecordDelivery(receipt).Committed {
		t.Fatal("deny+PostToolUse must not commit")
	}
}

func TestClaudeDelivery_EarliestHookAfterSpawn(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := makeSetup(t, "allow_once")
	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(5 * time.Second)
	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()
	resumeURL, posttoolURL := captureBridgeURLs(t, l)
	fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo hello"}`)
	firePostToolHook(t, posttoolURL, csid, tuid, tn, `{"command":"echo hello"}`)
	wg.Wait()
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("got %s", receipt.Outcome)
	}
}

func TestClaudeDelivery_CompositionTimeout(t *testing.T) {
	_, svc, _, claim, _, _, _, _ := makeSetup(t, "allow_once")
	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(10 * time.Millisecond)
	r := d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	if r.Outcome == term.DeliveryAccepted {
		t.Fatal("expected non-Accepted")
	}
}

func TestClaudeDelivery_ExitWithoutJoinedDeferredClearsIdentity(t *testing.T) {
	l, svc, _, _, _, _, _, _ := makeSetup(t, "allow_once")
	l.mu.Lock()
	if len(l.procs) > 0 {
		l.procs[0].StdoutW.Close()
	}
	l.mu.Unlock()
	time.Sleep(200 * time.Millisecond)
	if svc.Coordinator().IdentityCount() != 0 {
		t.Fatal("exit without joined deferred must clear identity")
	}
}

func TestClaudeDelivery_DeferredExitThenStopClearsIdentity(t *testing.T) {
	l, svc, _, _, _, _, _, _ := makeSetup(t, "allow_once")
	sid, _ := svc.CreateDetached("/tmp")
	_ = sid
	l.mu.Lock()
	if len(l.procs) > 0 {
		deferred := `{"type":"result","stop_reason":"tool_deferred","session_id":"claude-sess-allow_once","deferred_tool_use":{"id":"call_comp_allow_once","name":"Bash","input":{"command":"echo hello"}}}` + "\n"
		l.procs[0].StdoutW.Write([]byte(deferred))
		l.procs[0].StdoutW.Close()
	}
	l.mu.Unlock()
	time.Sleep(200 * time.Millisecond)
	if svc.Coordinator().IdentityCount() != 1 {
		t.Fatalf("identity must survive, got %d", svc.Coordinator().IdentityCount())
	}
	svc.Stop(sid, 1)
	if svc.Coordinator().IdentityCount() != 0 {
		t.Fatal("not cleared after stop")
	}
}

func TestClaudeDelivery_DeferredExitThenDeleteClearsIdentity(t *testing.T) {
	l, svc, _, _, _, _, _, _ := makeSetup(t, "allow_once")
	sid, _ := svc.CreateDetached("/tmp")
	_ = sid
	l.mu.Lock()
	if len(l.procs) > 0 {
		deferred := `{"type":"result","stop_reason":"tool_deferred","session_id":"claude-sess-allow_once","deferred_tool_use":{"id":"call_comp_allow_once","name":"Bash","input":{"command":"echo hello"}}}` + "\n"
		l.procs[0].StdoutW.Write([]byte(deferred))
		l.procs[0].StdoutW.Close()
	}
	l.mu.Unlock()
	time.Sleep(200 * time.Millisecond)
	if svc.Coordinator().IdentityCount() != 1 {
		t.Fatal("identity must survive")
	}
	svc.Delete(sid, 1)
	if svc.Coordinator().IdentityCount() != 0 {
		t.Fatal("not cleared after delete")
	}
}
func bytesEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── R6-B adversarial deny tests ──

func TestClaudeDelivery_DenyMutatedInputCannotCommit(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := makeSetup(t, "deny")
	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(2 * time.Second)
	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()
	resumeURL, _ := captureBridgeURLs(t, l)
	r := fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo hello"}`)
	if string(r) != string(term.ClaudeHookResponseBytes("deny")) {
		t.Fatalf("resume response: %s", r)
	}
	// CE-1 (known-bad): the event carries the correct session/tool identity but
	// a mutated tool_input whose canonical digest does NOT equal the stored one.
	// If the decoder passed ctx.inputDigest, MarkWitnessed would accept this.
	// The recomputed digest from {"command":"echo EVIL"} ≠ the stored digest
	// for {"command":"echo hello"}, so this must produce non-success.
	mutatedDenial := `{"type":"result","session_id":"` + csid + `","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"command":"echo EVIL"}}]}` + "\n"
	select {
	case <-l.resumeWCh:
	case <-time.After(time.Second):
		t.Fatal("resume writer not ready")
	}
	l.resumeW.Write([]byte(mutatedDenial))
	wg.Wait()
	if receipt.Outcome == term.DeliveryAccepted {
		t.Fatal("mutated tool_input must not witness — decoder recomputation is the enforcing boundary")
	}
}

func TestClaudeDelivery_DenyWithoutToolInputFailsClosed(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := makeSetup(t, "deny")
	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(2 * time.Second)
	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()
	resumeURL, _ := captureBridgeURLs(t, l)
	fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo hello"}`)
	// CE-2: old-style minimal event without tool_input — fails closed (no
	// witness). The pre-R6-B fixture this replaces could never have witnessed
	// on the real wire.
	oldStyle := `{"type":"result","stop_reason":"end_turn","session_id":"` + csid + `","permission_denials":[{"tool_name":"Bash","tool_use_id":"` + tuid + `"}]}` + "\n"
	select {
	case <-l.resumeWCh:
	case <-time.After(time.Second):
		t.Fatal("resume writer not ready")
	}
	l.resumeW.Write([]byte(oldStyle))
	wg.Wait()
	if receipt.Outcome == term.DeliveryAccepted {
		t.Fatal("denial without tool_input must be malformed and fail closed")
	}
}

func TestClaudeDelivery_DenyUnknownEntryFieldFailsClosed(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := makeSetup(t, "deny")
	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(2 * time.Second)
	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()
	resumeURL, _ := captureBridgeURLs(t, l)
	fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo hello"}`)
	// CE-3: unknown entry field "extra_field" — malformed, fail closed.
	unknownField := `{"type":"result","session_id":"` + csid + `","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"command":"echo hello"},"extra_field":"evil"}]}` + "\n"
	select {
	case <-l.resumeWCh:
	case <-time.After(time.Second):
		t.Fatal("resume writer not ready")
	}
	l.resumeW.Write([]byte(unknownField))
	wg.Wait()
	if receipt.Outcome == term.DeliveryAccepted {
		t.Fatal("denial with unknown entry field must be malformed")
	}
}
