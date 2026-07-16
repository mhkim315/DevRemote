package main

import (
	"fmt"
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
	mu      sync.Mutex
	allArgs [][]string
}

func (l *compFakeLauncher) Launch(_ string, argv []string) (term.ManagedProcess, error) {
	l.mu.Lock()
	l.allArgs = append(l.allArgs, append([]string(nil), argv...))
	l.mu.Unlock()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	return &compFakeProc{stdinR: stdinR, stdinW: stdinW, stdoutR: stdoutR, stdoutW: stdoutW, killed: make(chan struct{})}, nil
}

type compFakeProc struct {
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	killed  chan struct{}
	once    sync.Once
}

func (p *compFakeProc) Stdin() io.Writer  { return p.stdinW }
func (p *compFakeProc) Stdout() io.Reader { return p.stdoutR }
func (p *compFakeProc) Term() error       { return p.Kill() }
func (p *compFakeProc) Kill() error       { p.once.Do(func() { close(p.killed); p.stdinR.Close(); p.stdoutW.Close() }); return nil }
func (p *compFakeProc) Wait() error       { <-p.killed; return nil }
func (p *compFakeProc) PID() int          { return 0 }
func (p *compFakeProc) OpaqueID() string  { return "comp-fake" }

type compFakeAttestor struct{}

func (a *compFakeAttestor) Certify(_ string) error { return nil }

type providerSim struct {
	launcher   *compFakeLauncher
	svc        *term.ManagedClaudeService
	store      *term.AuthoritativeApprovalStore
	bridgeURLs struct {
		resume   string
		posttool string
	}
}

func newProviderSim(t *testing.T) *providerSim {
	t.Helper()
	l := &compFakeLauncher{}
	cfg := term.ClaudeEntryConfig{
		Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209",
		PinnedPath: "/tmp/fake-claude",
		PinnedDigest: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	svc := term.NewManagedClaudeService(cfg, l, &compFakeAttestor{})
	store := term.NewApprovalStore()
	svc.SetApprovalStore(store)
	return &providerSim{launcher: l, svc: svc, store: store}
}

func (s *providerSim) captureBridgeURLs(t *testing.T) {
	t.Helper()
	for i := 0; i < 100; i++ {
		time.Sleep(10 * time.Millisecond)
		s.launcher.mu.Lock()
		if len(s.launcher.allArgs) >= 2 {
			args := s.launcher.allArgs[1]
			var sp string
			for j, a := range args {
				if a == "--settings" && j+1 < len(args) {
					sp = args[j+1]
					break
				}
			}
			if sp != "" {
				hd := strings.TrimSuffix(sp, "/settings.json")
				s.bridgeURLs.resume = readHookURL(t, hd+"/hook_resume.sh")
				s.bridgeURLs.posttool = readHookURL(t, hd+"/hook_posttool.sh")
				s.launcher.mu.Unlock()
				return
			}
		}
		s.launcher.mu.Unlock()
	}
	t.Fatal("resume launch never observed")
}

func (s *providerSim) fireResumeHook(t *testing.T, sid, tuid, tn, inputJSON string) []byte {
	t.Helper()
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"%s","tool_input":%s,"hook_event_name":"PreToolUse"}`, sid, tuid, tn, inputJSON)
	resp, err := http.Post(s.bridgeURLs.resume, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("resume hook: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b
}

func (s *providerSim) firePostToolHook(t *testing.T, sid, tuid, tn, inputJSON string) {
	t.Helper()
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"%s","tool_input":%s,"hook_event_name":"PostToolUse"}`, sid, tuid, tn, inputJSON)
	resp, err := http.Post(s.bridgeURLs.posttool, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("posttool hook: %v", err)
	}
	resp.Body.Close()
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

func setupDeliveryScenario(t *testing.T, optionID string) (*providerSim, term.ClaimResult, term.RuntimeRef, string, string, string, string) {
	t.Helper()
	sim := newProviderSim(t)
	sid, err := sim.svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	rt := term.RuntimeRef{Adapter: "claude_headless", Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	aid := "claude-comp-" + optionID
	csid := "claude-sess-" + optionID
	tuid := "call_comp_" + optionID
	tn := "Bash"
	// Compute the correct input digest from the actual tool_input that
	// the hook will send, so ClaimWrite identity validation matches.
	testInput := `{"command":"echo hello"}`
	dgst := term.CanonicalDigest([]byte(testInput))
	sim.svc.Coordinator().ReserveIdentity(aid, csid, tuid, tn, dgst, sid, rt)

	ab := term.ClaudeHookResponseBytes("allow")
	db := term.ClaudeHookResponseBytes("deny")
	items := []term.ApprovalIngestItem{{
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
	}}
	sim.store.IngestObserved(term.ApprovalIngest{SessionID: sid, LaunchGen: 1, StreamGen: 0, Provider: "claude_headless", Version: "2.1.209", Items: items})
	reqCtx := term.RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "b2", Permissions: []string{"x"}}
	claim := sim.store.ClaimForExecution(term.ClaimRequest{SessionID: sid, ApprovalID: aid, OptionID: optionID, Input: "", Runtime: rt, Requester: reqCtx, IdempotencyKey: "test.comp." + optionID})
	if claim.Outcome != term.ClaimGranted {
		t.Fatalf("ClaimForExecution: %s", claim.Outcome)
	}
	return sim, claim, rt, csid, tuid, tn, sid
}

// ── R4-0 tests ──

func TestClaudeDelivery_CompositionAllowAccepted(t *testing.T) {
	sim, claim, _, csid, tuid, tn, _ := setupDeliveryScenario(t, "allow_once")
	d := term.NewClaudeManagedApprovalDelivery(sim.svc)
	d.SetPollTimeout(5 * time.Second)

	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()

	sim.captureBridgeURLs(t)
	r := sim.fireResumeHook(t, csid, tuid, tn, `{"command":"echo hello"}`)
	if !bytesEq(r, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("unexpected resume response: %s", r)
	}
	sim.firePostToolHook(t, csid, tuid, tn, `{"command":"echo hello"}`)
	wg.Wait()

	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("expected accepted, got %s", receipt.Outcome)
	}
	if receipt.DeliveredPayloadDigest != claim.Binding.PayloadDigest {
		t.Fatal("receipt digest mismatch")
	}
	if !sim.store.RecordDelivery(receipt).Committed {
		t.Fatal("RecordDelivery not committed")
	}
}

func TestClaudeDelivery_CompositionDenyAccepted(t *testing.T) {
	sim, claim, _, csid, tuid, tn, _ := setupDeliveryScenario(t, "deny")
	d := term.NewClaudeManagedApprovalDelivery(sim.svc)
	d.SetPollTimeout(5 * time.Second)

	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()

	sim.captureBridgeURLs(t)
	r := sim.fireResumeHook(t, csid, tuid, tn, `{"command":"echo hello"}`)
	if !bytesEq(r, term.ClaudeHookResponseBytes("deny")) {
		t.Fatalf("unexpected resume response: %s", r)
	}
	sim.firePostToolHook(t, csid, tuid, tn, `{"command":"echo hello"}`)
	wg.Wait()

	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("expected accepted for deny, got %s", receipt.Outcome)
	}
	if !sim.store.RecordDelivery(receipt).Committed {
		t.Fatal("RecordDelivery not committed")
	}
}

func TestClaudeDelivery_EarliestHookAfterSpawn(t *testing.T) {
	sim, claim, _, csid, tuid, tn, _ := setupDeliveryScenario(t, "allow_once")
	d := term.NewClaudeManagedApprovalDelivery(sim.svc)
	d.SetPollTimeout(5 * time.Second)

	var receipt term.DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	}()

	sim.captureBridgeURLs(t)
	sim.fireResumeHook(t, csid, tuid, tn, `{"command":"echo hello"}`)
	sim.firePostToolHook(t, csid, tuid, tn, `{"command":"echo hello"}`)
	wg.Wait()

	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("earliest hook should succeed: got %s", receipt.Outcome)
	}
}

func TestClaudeDelivery_DeferredExitPreservesResumeIdentity(t *testing.T) {
	sim, _, _, _, _, _, sid := setupDeliveryScenario(t, "allow_once")
	if sim.svc.Coordinator().IdentityCount() != 1 {
		t.Fatal("identity should exist")
	}
	sim.svc.SimulateGracefulExit(sid, 1)
	if sim.svc.Coordinator().IdentityCount() != 1 {
		t.Fatalf("identity must survive deferred exit, got %d", sim.svc.Coordinator().IdentityCount())
	}
}

func TestClaudeDelivery_StopAfterDeferredExitInvalidatesIdentity(t *testing.T) {
	sim, _, _, _, _, _, sid := setupDeliveryScenario(t, "allow_once")
	// Explicit stop should clear the identity (destructive lifecycle action).
	sim.svc.Stop(sid, 1)
	if sim.svc.Coordinator().IdentityCount() != 0 {
		t.Fatal("identity cleared after explicit stop")
	}
}

func TestClaudeDelivery_CompositionTimeout(t *testing.T) {
	sim, claim, _, _, _, _, _ := setupDeliveryScenario(t, "allow_once")
	d := term.NewClaudeManagedApprovalDelivery(sim.svc)
	d.SetPollTimeout(10 * time.Millisecond)
	r := d.Deliver(term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	if r.Outcome == term.DeliveryAccepted {
		t.Fatal("expected non-Accepted for timeout")
	}
}
