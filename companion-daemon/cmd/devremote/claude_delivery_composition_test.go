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
	svc.Coordinator().ReserveIdentity(aid, csid, tuid, tn, dgst, "", sid, rt)

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

// captureInitialHookURL extracts the initial hook bridge URL from the hook script
// written during the first CreateDetached. The launcher must have at least one
// recorded launch.
func captureInitialHookURL(t *testing.T, launcher *compFakeLauncher) string {
	t.Helper()
	launcher.mu.Lock()
	if len(launcher.allArgs) < 1 {
		launcher.mu.Unlock()
		t.Fatal("initial launch never observed")
	}
	args := launcher.allArgs[0]
	launcher.mu.Unlock()
	for j, a := range args {
		if a == "--settings" && j+1 < len(args) {
			hd := strings.TrimSuffix(args[j+1], "/settings.json")
			return readHookURL(t, hd+"/hook.sh")
		}
	}
	t.Fatal("settings arg not found in initial launch")
	return ""
}

// fireInitialHook POSTs a PreToolUse to the bridge's /hook endpoint,
// simulating what the Claude hook command would do.
func fireInitialHook(t *testing.T, url, sid, tuid, tn, inputJSON string) {
	t.Helper()
	body := `{"session_id":"` + sid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PreToolUse"}`
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("initial hook: %v", err)
	}
	resp.Body.Close()
}

func TestClaudeDelivery_ExitWithoutJoinedDeferredClearsIdentity(t *testing.T) {
	launcher, svc, _ := newProviderSim(t)
	sid, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}

	// Create a pending observation via the real bridge /hook endpoint,
	// then write a deferred event to join it, creating an identity.
	hookURL := captureInitialHookURL(t, launcher)
	csid := "claude-sess-nojoin"
	tuid := "call_nojoin"
	tn := "Bash"
	inputJSON := `{"command":"echo hello"}`
	fireInitialHook(t, hookURL, csid, tuid, tn, inputJSON)

	// Write a deferred event that does NOT match (different tuid).
	// joinDeferred won't fire, so the identity from ReserveIdentity
	// in makeSetup path is never created.
	deferred := `{"type":"result","stop_reason":"tool_deferred","session_id":"` + csid + `","deferred_tool_use":{"id":"call_WRONG","name":"Bash","input":{"command":"echo hello"}}}` + "\n"
	launcher.mu.Lock()
	if len(launcher.procs) >= 1 {
		launcher.procs[0].StdoutW.Write([]byte(deferred))
		launcher.procs[0].StdoutW.Close()
	}
	launcher.mu.Unlock()

	svc.WaitExited(sid)
	if svc.Coordinator().IdentityCount() != 0 {
		t.Fatal("exit without joined deferred must clear identity")
	}
}

func TestClaudeDelivery_DeferredExitThenStopClearsIdentity(t *testing.T) {
	launcher, svc, _ := newProviderSim(t)
	sid, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}

	hookURL := captureInitialHookURL(t, launcher)
	csid := "claude-sess-stop"
	tuid := "call_stop"
	tn := "Bash"
	inputJSON := `{"command":"echo hello"}`
	fireInitialHook(t, hookURL, csid, tuid, tn, inputJSON)

	// Write a matching deferred event → joinDeferred fires → identity created.
	deferred := `{"type":"result","stop_reason":"tool_deferred","session_id":"` + csid + `","deferred_tool_use":{"id":"` + tuid + `","name":"` + tn + `","input":` + inputJSON + `}}` + "\n"
	launcher.mu.Lock()
	if len(launcher.procs) >= 1 {
		launcher.procs[0].StdoutW.Write([]byte(deferred))
		launcher.procs[0].StdoutW.Close()
	}
	launcher.mu.Unlock()

	// Deterministic barrier: block until the runtime exits.
	svc.WaitExited(sid)

	if svc.Coordinator().IdentityCount() != 1 {
		t.Fatalf("identity must survive deferred exit, got %d", svc.Coordinator().IdentityCount())
	}
	if err := svc.Stop(sid, 1); err != nil {
		t.Fatal(err)
	}
	if svc.Coordinator().IdentityCount() != 0 {
		t.Fatal("identity not cleared after stop")
	}
}

func TestClaudeDelivery_DeferredExitThenKillClearsIdentity(t *testing.T) {
	launcher, svc, _ := newProviderSim(t)
	sid, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}

	hookURL := captureInitialHookURL(t, launcher)
	csid := "claude-sess-kill"
	tuid := "call_kill"
	tn := "Bash"
	inputJSON := `{"command":"echo hello"}`
	fireInitialHook(t, hookURL, csid, tuid, tn, inputJSON)

	deferred := `{"type":"result","stop_reason":"tool_deferred","session_id":"` + csid + `","deferred_tool_use":{"id":"` + tuid + `","name":"` + tn + `","input":` + inputJSON + `}}` + "\n"
	launcher.mu.Lock()
	if len(launcher.procs) >= 1 {
		launcher.procs[0].StdoutW.Write([]byte(deferred))
		launcher.procs[0].StdoutW.Close()
	}
	launcher.mu.Unlock()

	svc.WaitExited(sid)

	if svc.Coordinator().IdentityCount() != 1 {
		t.Fatalf("identity must survive deferred exit, got %d", svc.Coordinator().IdentityCount())
	}
	if err := svc.Kill(sid, 1); err != nil {
		t.Fatal(err)
	}
	if svc.Coordinator().IdentityCount() != 0 {
		t.Fatal("identity not cleared after kill")
	}
}

func TestClaudeDelivery_DeferredExitThenDeleteClearsIdentity(t *testing.T) {
	launcher, svc, _ := newProviderSim(t)
	sid, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}

	hookURL := captureInitialHookURL(t, launcher)
	csid := "claude-sess-delete"
	tuid := "call_delete"
	tn := "Bash"
	inputJSON := `{"command":"echo hello"}`
	fireInitialHook(t, hookURL, csid, tuid, tn, inputJSON)

	deferred := `{"type":"result","stop_reason":"tool_deferred","session_id":"` + csid + `","deferred_tool_use":{"id":"` + tuid + `","name":"` + tn + `","input":` + inputJSON + `}}` + "\n"
	launcher.mu.Lock()
	if len(launcher.procs) >= 1 {
		launcher.procs[0].StdoutW.Write([]byte(deferred))
		launcher.procs[0].StdoutW.Close()
	}
	launcher.mu.Unlock()

	svc.WaitExited(sid)

	if svc.Coordinator().IdentityCount() != 1 {
		t.Fatal("identity must survive deferred exit")
	}
	// Delete requires Exited. The deferred exit already terminated the runtime.
	// No Stop pre-call — proves Delete cleanup independently.
	if err := svc.Delete(sid, 1); err != nil {
		t.Fatal(err)
	}
	if svc.Coordinator().IdentityCount() != 0 {
		t.Fatal("identity not cleared after delete")
	}
}

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

// ── P3 catalog composition tests ──

// catalogMakeSetup creates a controlled actionable fixture with the catalog
// probe command and the correct catalogActionID in the coordinator identity.
// It checks every setup step so a silent failure cannot masquerade as evidence.
func catalogMakeSetup(t *testing.T, optionID string) (*compFakeLauncher, *term.ManagedClaudeService, *term.AuthoritativeApprovalStore, term.ClaimResult, term.RuntimeRef, string, string, string) {
	t.Helper()
	launcher, svc, store := newProviderSim(t)
	sid, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	rt := term.RuntimeRef{Adapter: "claude_headless", Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	aid := "claude-comp-cat-" + optionID
	csid := "claude-sess-cat-" + optionID
	tuid := "call_comp_cat_" + optionID
	dgst := term.CanonicalDigest([]byte(`{"command":"echo pokitclaudeapprovalprobe"}`))

	// Catalog-bearing identity reservation.
	if !svc.Coordinator().ReserveIdentity(aid, csid, tuid, "Bash", dgst, "claude.bash.approval_probe.v1", sid, rt) {
		t.Fatal("ReserveIdentity failed for catalog setup")
	}

	ab := term.ClaudeHookResponseBytes("allow")
	db := term.ClaudeHookResponseBytes("deny")
	admitted := store.IngestObserved(term.ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0, Provider: "claude_headless", Version: "2.1.209",
		Items: []term.ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: aid, SessionID: sid, AgentKind: "claude_headless",
				Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
				Options: []agent.InteractionOption{
					{ID: "allow_once", Label: "Allow once", Kind: "approve"},
					{ID: "deny", Label: "Deny", Kind: "reject"},
				},
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      true,
			CatalogActionID: "claude.bash.approval_probe.v1",
			DeliveryMaterial: []term.ApprovalDeliveryMaterial{
				{OptionID: "allow_once", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: ab},
				{OptionID: "deny", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: db},
			},
		}},
	})
	if !admitted {
		t.Fatal("IngestObserved failed for catalog setup")
	}

	// Verify DTO shows catalog summary BEFORE claim (setup invariant).
	dtos := store.ListSafe(sid)
	var found bool
	for _, d := range dtos {
		if d.ID == aid {
			found = true
			if d.Summary != "Run Claude approval verification probe" {
				t.Fatalf("catalog setup: DTO summary = %q", d.Summary)
			}
		}
	}
	if !found {
		t.Fatal("catalog setup: approval not found in ListSafe")
	}

	reqCtx := term.RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "b2", Permissions: []string{"x"}}
	claim := store.ClaimForExecution(term.ClaimRequest{SessionID: sid, ApprovalID: aid, OptionID: optionID, Input: "", Runtime: rt, Requester: reqCtx, IdempotencyKey: "test.comp.cat." + optionID})
	if claim.Outcome != term.ClaimGranted {
		t.Fatalf("ClaimForExecution: %s", claim.Outcome)
	}
	return launcher, svc, store, claim, rt, csid, tuid, "Bash"
}

func TestClaudeDelivery_CatalogAllowAccepted(t *testing.T) {
	l, svc, store, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
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
	r := fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo pokitclaudeapprovalprobe"}`)
	if !bytesEq(r, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("resume response: %s", r)
	}
	firePostToolHook(t, posttoolURL, csid, tuid, tn, `{"command":"echo pokitclaudeapprovalprobe"}`)
	wg.Wait()
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("expected accepted, got %s", receipt.Outcome)
	}
	if !store.RecordDelivery(receipt).Committed {
		t.Fatal("not committed")
	}
}

func TestClaudeDelivery_CatalogDenyAccepted(t *testing.T) {
	l, svc, store, claim, _, csid, tuid, tn := catalogMakeSetup(t, "deny")
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
	r := fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo pokitclaudeapprovalprobe"}`)
	if string(r) != string(term.ClaudeHookResponseBytes("deny")) {
		t.Fatalf("resume response: %s", r)
	}
	// Write denial witness deterministically via the resume pipe
	// (same pattern as existing CompositionDenyAccepted test).
	denialJSON := `{"type":"result","session_id":"` + csid + `","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"}}]}` + "\n"
	select {
	case <-l.resumeWCh:
	case <-time.After(time.Second):
		t.Fatal("resume writer not ready")
	}
	l.resumeW.Write([]byte(denialJSON))
	wg.Wait()
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("expected accepted for deny, got %s", receipt.Outcome)
	}
	if !store.RecordDelivery(receipt).Committed {
		t.Fatal("not committed")
	}
}

func TestClaudeDelivery_CatalogDenyWrongWitnessFails(t *testing.T) {
	// A catalog deny with a PostToolUse witness (wrong kind for deny)
	// must not be accepted.
	l, svc, store, claim, _, csid, tuid, tn := catalogMakeSetup(t, "deny")
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
	fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo pokitclaudeapprovalprobe"}`)
	// Wrong witness: PostToolUse on a deny decision.
	firePostToolHook(t, posttoolURL, csid, tuid, tn, `{"command":"echo pokitclaudeapprovalprobe"}`)
	wg.Wait()
	if receipt.Outcome == term.DeliveryAccepted {
		t.Fatal("PostToolUse witness must not succeed for deny decision")
	}
	_ = store
}

func TestClaudeDelivery_CatalogAllowWrongWitnessFails(t *testing.T) {
	// A catalog allow with a permission_denials witness (wrong kind for allow)
	// must not be accepted.
	l, svc, store, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
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
	fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo pokitclaudeapprovalprobe"}`)
	// Wrong witness: permission_denials on an allow decision.
	denialJSON := `{"type":"result","session_id":"` + csid + `","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"}}]}` + "\n"
	select {
	case <-l.resumeWCh:
	case <-time.After(time.Second):
		t.Fatal("resume writer not ready")
	}
	l.resumeW.Write([]byte(denialJSON))
	wg.Wait()
	if receipt.Outcome == term.DeliveryAccepted {
		t.Fatal("permission_denials witness must not succeed for allow decision")
	}
	_ = store
}

func TestClaudeDelivery_CatalogTimeoutFails(t *testing.T) {
	// Delivery with no witness must fail (timeout), even with catalog match.
	l, svc, store, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(500 * time.Millisecond)
	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()
	resumeURL, _ := captureBridgeURLs(t, l)
	fireResumeHook(t, resumeURL, csid, tuid, tn, `{"command":"echo pokitclaudeapprovalprobe"}`)
	// No witness — timeout.
	wg.Wait()
	if receipt.Outcome == term.DeliveryAccepted {
		t.Fatal("timeout must not succeed")
	}
	_ = store
	_ = l
}
