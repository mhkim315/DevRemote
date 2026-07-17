// C3D-A tests — atomic Claude activation, deepest-boundary admission,
// current-runtime resolution, composition-boundary dispatch, and the
// per-incarnation launch-certification binding.
//
// Deterministic production-path coverage for handoff §5 items 1-6 plus the
// two reviewer-required evidence points: the resume launch-certification
// tuple is recorded with the delivery attempt's runtime (and an uncertified
// resume never reaches the witness stage), and cross-provider requests are
// rejected before any provider write in Codex-only, Claude-only and
// both-installed compositions. No live model turn, no mobile code.
package term

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

const activationCatalogID = "claude.bash.approval_probe.v1"

// newInstalledClaudeService builds a service and commits the ONE activation
// transition before any runtime exists.
func newInstalledClaudeService(t *testing.T, launcher ManagedLauncher) (*ManagedClaudeService, *AuthoritativeApprovalStore, ApprovalDelivery, func(string) (RuntimeRef, bool)) {
	t.Helper()
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	delivery, runtimeOf, err := svc.InstallApprovalExecution(store)
	if err != nil {
		t.Fatalf("InstallApprovalExecution: %v", err)
	}
	if delivery == nil || runtimeOf == nil {
		t.Fatal("install must return delivery + resolver")
	}
	if !svc.ApprovalExecutionInstalled() {
		t.Fatal("ApprovalExecutionInstalled must report true")
	}
	return svc, store, delivery, runtimeOf
}

// driveProbeToPending fires the REAL /hook bridge with the exact catalog
// probe and joins the deferred result, yielding the pending approval ID.
func driveProbeToPending(t *testing.T, svc *ManagedClaudeService, id, claudeSessionID, toolUseID string) string {
	t.Helper()
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()
	if rt == nil {
		t.Fatal("runtime is nil")
	}
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, claudeSessionID, toolUseID)
	postHook(t, rt, body)
	rt.processLine([]byte(deferredStreamJSON(claudeSessionID, toolUseID, "Bash", `{"command":"echo pokitclaudeapprovalprobe"}`)))
	rt.turnMu.Lock()
	defer rt.turnMu.Unlock()
	if len(rt.activeApprovals) != 1 {
		t.Fatalf("expected 1 active approval, got %d", len(rt.activeApprovals))
	}
	return rt.activeApprovals[0].approvalID
}

// ── §1 install transition ──

func TestClaudeInstall_SucceedsBeforeFirstRuntime(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, store, _, runtimeOf := newInstalledClaudeService(t, launcher)
	t.Cleanup(func() { launcher.closeStream() })

	// All returned components belong to the same service/store: a runtime
	// created AFTER install ingests actionable records into the installed
	// store, and the returned resolver resolves it.
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	aid := driveProbeToPending(t, svc, id, "sess-install-ok", "call_install_ok")
	snap, ok := store.LookupRecord(id, aid)
	if !ok || !snap.Actionable {
		t.Fatalf("record must be actionable in the installed store: ok=%v snap=%+v", ok, snap)
	}
	ref, ok := runtimeOf(id)
	if !ok || ref.Adapter != claudeHeadlessAdapter || ref.LaunchGen != 1 || ref.StreamGen != 0 {
		t.Fatalf("returned resolver must resolve the live runtime: ok=%v ref=%+v", ok, ref)
	}
}

func TestClaudeInstall_PreconditionFailuresMutateNothing(t *testing.T) {
	store := NewApprovalStore()

	cases := []struct {
		name string
		svc  func() *ManagedClaudeService
		st   *AuthoritativeApprovalStore
		want string
	}{
		{"nil store", func() *ManagedClaudeService {
			return NewManagedClaudeService(testCfg(), &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
		}, nil, "no approval store"},
		{"wrong authority version", func() *ManagedClaudeService {
			cfg := testCfg()
			cfg.AuthorityVersion = "2.1.210"
			return NewManagedClaudeService(cfg, &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
		}, store, "authority version"},
		{"wrong pinned version", func() *ManagedClaudeService {
			cfg := testCfg()
			cfg.Version = "2.1.210"
			return NewManagedClaudeService(cfg, &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
		}, store, "pinned version"},
		{"empty pinned path", func() *ManagedClaudeService {
			cfg := testCfg()
			cfg.PinnedPath = ""
			return NewManagedClaudeService(cfg, &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
		}, store, "pinned path"},
		{"empty pinned digest", func() *ManagedClaudeService {
			cfg := testCfg()
			cfg.PinnedDigest = ""
			return NewManagedClaudeService(cfg, &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
		}, store, "pinned digest"},
		{"malformed pinned digest", func() *ManagedClaudeService {
			cfg := testCfg()
			cfg.PinnedDigest = strings.Repeat("Z", 64)
			return NewManagedClaudeService(cfg, &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
		}, store, "pinned digest"},
		{"different store configured", func() *ManagedClaudeService {
			s := NewManagedClaudeService(testCfg(), &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
			if err := s.SetApprovalStore(NewApprovalStore()); err != nil {
				t.Fatal(err)
			}
			return s
		}, store, "different approval store"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := tc.svc()
			d, r, err := svc.InstallApprovalExecution(tc.st)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
			if d != nil || r != nil {
				t.Fatal("failed install must return nil components")
			}
			if svc.ApprovalExecutionInstalled() {
				t.Fatal("failed install must not set actionable")
			}
		})
	}
}

func TestClaudeInstall_UncertifiedPlatformFails(t *testing.T) {
	prevOS, prevArch := launchOS, launchArch
	t.Cleanup(func() { launchOS, launchArch = prevOS, prevArch })
	launchOS, launchArch = "linux", "amd64"

	svc := NewManagedClaudeService(testCfg(), &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
	_, _, err := svc.InstallApprovalExecution(NewApprovalStore())
	if err == nil || !strings.Contains(err.Error(), "platform") {
		t.Fatalf("want platform error, got %v", err)
	}
	if svc.ApprovalExecutionInstalled() {
		t.Fatal("uncertified platform must not install")
	}
}

func TestClaudeInstall_ShutdownFails(t *testing.T) {
	svc := NewManagedClaudeService(testCfg(), &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
	svc.mu.Lock()
	svc.closing = true
	svc.mu.Unlock()
	if _, _, err := svc.InstallApprovalExecution(NewApprovalStore()); err == nil || !strings.Contains(err.Error(), "shutting down") {
		t.Fatalf("want shutting-down error, got %v", err)
	}
}

func TestClaudeInstall_AfterRuntimeFails(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { launcher.closeStream() })
	if _, err := svc.CreateDetached("/tmp"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.InstallApprovalExecution(store); err == nil || !strings.Contains(err.Error(), "precede the first managed runtime") {
		t.Fatalf("want precede-first-runtime error, got %v", err)
	}
	if svc.ApprovalExecutionInstalled() {
		t.Fatal("late install must not set actionable")
	}
}

func TestClaudeInstall_DoubleInstallFails(t *testing.T) {
	svc, store, _, _ := newInstalledClaudeService(t, &fakeClaudeLauncher{})
	if _, _, err := svc.InstallApprovalExecution(store); err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("want already-installed error, got %v", err)
	}
}

// KB-1: a failed install leaves NO Claude action path. The observation store
// binding (configured earlier) remains; actionability, options and RuntimeOf
// stay off, and the record can never be claimed.
func TestClaudeInstall_KB1_FailureLeavesNoActionPath(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	cfg := testCfg()
	cfg.AuthorityVersion = "2.1.210" // not certified
	cfg.Version = "2.1.210"
	svc := NewManagedClaudeService(cfg, launcher, &fakeClaudeAttestor{})
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { launcher.closeStream() })

	if _, _, err := svc.InstallApprovalExecution(store); err == nil {
		t.Fatal("install must fail on a non-certified version")
	}

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()
	body := fmt.Sprintf(`{"session_id":"s-kb1","tool_use_id":"call_kb1","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse"}`)
	postHook(t, rt, body)
	rt.processLine([]byte(deferredStreamJSON("s-kb1", "call_kb1", "Bash", `{"command":"echo pokitclaudeapprovalprobe"}`)))

	dtos := store.ListSafe(id)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 observation record, got %d", len(dtos))
	}
	if dtos[0].Actionable || len(dtos[0].Options) != 0 {
		t.Fatalf("KB-1: record must stay non-actionable with zero options: %+v", dtos[0])
	}
	if _, ok := svc.RuntimeOf(id); ok {
		t.Fatal("KB-1: RuntimeOf must not resolve on an uninstalled service")
	}
	claim := store.ClaimForExecution(ClaimRequest{
		SessionID: id, ApprovalID: dtos[0].ID, OptionID: "allow_once",
		Runtime:        RuntimeRef{Adapter: claudeHeadlessAdapter, Version: cfg.AuthorityVersion, LaunchGen: 1, StreamGen: 0},
		Requester:      RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt", Permissions: []string{claudeRequiredPerm}},
		IdempotencyKey: "kb1.k",
	})
	if claim.Outcome != ClaimNotActionable {
		t.Fatalf("KB-1: claim must be not_actionable, got %s", claim.Outcome)
	}
}

// Handoff §5 item 3: concurrent install/create has one defined winner and no
// partially actionable record. The create wins here (its epoch is allocated
// before the barrier), so the late install fails and the record stays
// non-actionable.
func TestClaudeInstall_ConcurrentCreateHasOneWinner(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { launcher.closeStream() })

	atBarrier := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	svc.createBarrier = func(stage string) {
		if stage == "pre-spawn" {
			once.Do(func() {
				close(atBarrier)
				<-release
			})
		}
	}

	var id string
	var createErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		id, createErr = svc.CreateDetached("/tmp")
	}()
	<-atBarrier

	// The create already allocated its epoch: the install must lose.
	_, _, err := svc.InstallApprovalExecution(store)
	if err == nil || !strings.Contains(err.Error(), "precede the first managed runtime") {
		t.Fatalf("install racing an in-flight create must fail, got %v", err)
	}
	close(release)
	<-done
	if createErr != nil {
		t.Fatalf("CreateDetached: %v", createErr)
	}

	// No partially actionable record: the runtime ingests non-actionable.
	aid := driveProbeToPending(t, svc, id, "sess-race", "call_race")
	snap, ok := store.LookupRecord(id, aid)
	if !ok || snap.Actionable || len(snap.Options) != 0 {
		t.Fatalf("race loser must leave record non-actionable: ok=%v snap=%+v", ok, snap)
	}
	if svc.ApprovalExecutionInstalled() {
		t.Fatal("losing install must not set actionable")
	}
}

// ── §3/§4 catalog-match actionable ingestion + admission boundary ──

func TestClaudeActionable_CatalogMatchExactSummaryAndOptions(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, store, _, _ := newInstalledClaudeService(t, launcher)
	t.Cleanup(func() { launcher.closeStream() })

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	aid := driveProbeToPending(t, svc, id, "sess-actionable", "call_actionable")

	dtos := store.ListSafe(id)
	if len(dtos) != 1 || dtos[0].ID != aid {
		t.Fatalf("expected the pending record, got %+v", dtos)
	}
	d := dtos[0]
	if !d.Actionable {
		t.Fatal("catalog match on an active runtime must be actionable")
	}
	if d.Summary != "Run Claude approval verification probe" {
		t.Fatalf("summary = %q", d.Summary)
	}
	if len(d.Options) != 2 {
		t.Fatalf("want exactly 2 options, got %d", len(d.Options))
	}
	byID := map[string]SafeOptionDTO{d.Options[0].ID: d.Options[0], d.Options[1].ID: d.Options[1]}
	if byID["allow_once"].Kind != "approve" || byID["allow_once"].Label != "Approve" {
		t.Fatalf("allow_once option projection: %+v", byID["allow_once"])
	}
	if byID["deny"].Kind != "reject" || byID["deny"].Label != "Reject" {
		t.Fatalf("deny option projection: %+v", byID["deny"])
	}
}

func TestClaudeActionable_NonCatalogStaysNonActionable(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, store, _, _ := newInstalledClaudeService(t, launcher)
	t.Cleanup(func() { launcher.closeStream() })

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()
	// Non-catalog command through the REAL hook path on an ACTIVE runtime.
	body := `{"session_id":"s-noncat","tool_use_id":"call_noncat","tool_name":"Bash","tool_input":{"command":"echo hello"},"hook_event_name":"PreToolUse"}`
	postHook(t, rt, body)
	rt.processLine([]byte(deferredStreamJSON("s-noncat", "call_noncat", "Bash", `{"command":"echo hello"}`)))

	dtos := store.ListSafe(id)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 record, got %d", len(dtos))
	}
	if dtos[0].Actionable || len(dtos[0].Options) != 0 {
		t.Fatalf("non-catalog observation must stay non-actionable: %+v", dtos[0])
	}
	if dtos[0].Summary == "Run Claude approval verification probe" {
		t.Fatal("non-catalog observation must not carry the catalog summary")
	}
}

// forgedIngest builds an actionable claude_headless ingest and lets one
// mutation forge a field. The Store admission boundary must drop it.
func forgedIngest(mutate func(*ApprovalIngest, *ApprovalIngestItem)) ApprovalIngest {
	item := ApprovalIngestItem{
		Approval: agent.AgentApproval{
			ID: "claude-forged", SessionID: "claude_headless:s1", AgentKind: claudeHeadlessAdapter,
			Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
			Options: claudeCertifiedOptions(),
		},
		Provenance:       contract.ProvenanceProviderHook,
		Actionable:       true,
		RequiredPerm:     claudeRequiredPerm,
		CatalogActionID:  activationCatalogID,
		DeliveryMaterial: claudeCertifiedDeliveryMaterial(),
	}
	in := ApprovalIngest{
		SessionID: "claude_headless:s1", LaunchGen: 1, StreamGen: 0,
		Provider: claudeHeadlessAdapter, Version: "2.1.209",
		Items: []ApprovalIngestItem{item},
	}
	mutate(&in, &in.Items[0])
	return in
}

func TestClaudeAdmission_ExactTupleAdmitted(t *testing.T) {
	store := NewApprovalStore()
	if !store.IngestObserved(forgedIngest(func(*ApprovalIngest, *ApprovalIngestItem) {})) {
		t.Fatal("the exact certified tuple must be admitted")
	}
	dtos := store.ListSafe("claude_headless:s1")
	if len(dtos) != 1 || !dtos[0].Actionable || len(dtos[0].Options) != 2 {
		t.Fatalf("admitted record projection: %+v", dtos)
	}
}

func TestClaudeAdmission_ForgedValuesRejected(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ApprovalIngest, *ApprovalIngestItem)
	}{
		{"missing catalog id", func(_ *ApprovalIngest, it *ApprovalIngestItem) { it.CatalogActionID = "" }},
		{"unknown catalog id", func(_ *ApprovalIngest, it *ApprovalIngestItem) { it.CatalogActionID = "claude.bash.other.v1" }},
		{"version mismatch", func(in *ApprovalIngest, _ *ApprovalIngestItem) { in.Version = "2.1.210" }},
		{"stream generation nonzero", func(in *ApprovalIngest, _ *ApprovalIngestItem) { in.StreamGen = 1 }},
		{"third option", func(_ *ApprovalIngest, it *ApprovalIngestItem) {
			it.Approval.Options = append(it.Approval.Options, agent.InteractionOption{ID: "always", Label: "Always", Kind: "approve"})
		}},
		{"renamed option id", func(_ *ApprovalIngest, it *ApprovalIngestItem) { it.Approval.Options[0].ID = "allow_always" }},
		{"forged option label", func(_ *ApprovalIngest, it *ApprovalIngestItem) { it.Approval.Options[0].Label = "Allow forever" }},
		{"forged option kind", func(_ *ApprovalIngest, it *ApprovalIngestItem) { it.Approval.Options[1].Kind = "approve" }},
		{"option input schema", func(_ *ApprovalIngest, it *ApprovalIngestItem) {
			it.Approval.Options[0].Input = &agent.InputSchema{Required: true}
		}},
		{"option payload", func(_ *ApprovalIngest, it *ApprovalIngestItem) { it.Approval.Options[0].Payload = "y\n" }},
		{"mutated material byte", func(_ *ApprovalIngest, it *ApprovalIngestItem) {
			b := append([]byte(nil), it.DeliveryMaterial[0].ResponseBytes...)
			b[len(b)-2] ^= 0x01
			it.DeliveryMaterial[0].ResponseBytes = b
		}},
		{"wrong material schema", func(_ *ApprovalIngest, it *ApprovalIngestItem) {
			it.DeliveryMaterial[0].SchemaVersion = "claude.pretooluse.decision.v2"
		}},
		{"missing deny material", func(_ *ApprovalIngest, it *ApprovalIngestItem) { it.DeliveryMaterial = it.DeliveryMaterial[:1] }},
		{"swapped decisions", func(_ *ApprovalIngest, it *ApprovalIngestItem) {
			it.DeliveryMaterial[0].ResponseBytes, it.DeliveryMaterial[1].ResponseBytes =
				it.DeliveryMaterial[1].ResponseBytes, it.DeliveryMaterial[0].ResponseBytes
		}},
		{"wrong required permission", func(_ *ApprovalIngest, it *ApprovalIngestItem) { it.RequiredPerm = "sessions:read" }},
		{"empty required permission", func(_ *ApprovalIngest, it *ApprovalIngestItem) { it.RequiredPerm = "" }},
		{"non-hook provenance", func(_ *ApprovalIngest, it *ApprovalIngestItem) { it.Provenance = contract.ProvenanceProviderProtocol }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewApprovalStore()
			if store.IngestObserved(forgedIngest(tc.mutate)) {
				t.Fatal("forged actionable claude item must be rejected at the Store admission boundary")
			}
			if len(store.ListSafe("claude_headless:s1")) != 0 {
				t.Fatal("rejected item must create no record")
			}
			// The rejected item must not have created a session or advanced
			// the generation high-water: a subsequent exact-tuple ingest at
			// the SAME generation is still admitted.
			if !store.IngestObserved(forgedIngest(func(*ApprovalIngest, *ApprovalIngestItem) {})) {
				t.Fatal("rejection must not consume generation authority")
			}
		})
	}
}

// KB-2: making Claude actionable on provider name alone — an actionable item
// with fabricated options and NO catalog identity/material (the shape a
// provider-wide provenActionMapping switch would produce) — is dropped at
// the deepest admission boundary even from a trusted internal caller.
func TestClaudeAdmission_KB2_ProviderWideActionabilityRejected(t *testing.T) {
	store := NewApprovalStore()
	in := forgedIngest(func(_ *ApprovalIngest, it *ApprovalIngestItem) {
		it.CatalogActionID = ""
		it.DeliveryMaterial = nil
	})
	if store.IngestObserved(in) {
		t.Fatal("KB-2: provider-name-only actionability must be rejected")
	}
	if len(store.ListSafe("claude_headless:s1")) != 0 {
		t.Fatal("KB-2: no record, no CTA, no claim path")
	}
	// Non-actionable observation with the same shape is still admitted.
	obs := forgedIngest(func(_ *ApprovalIngest, it *ApprovalIngestItem) {
		it.Actionable = false
		it.RequiredPerm = ""
		it.CatalogActionID = ""
		it.DeliveryMaterial = nil
		it.Approval.Options = nil
	})
	if !store.IngestObserved(obs) {
		t.Fatal("non-actionable observation must keep the frozen admission rules")
	}
}

// Codex actionable admission is byte-for-byte the accepted SP1 behavior:
// the Claude policy must not constrain it.
func TestClaudeAdmission_CodexActionableUnchanged(t *testing.T) {
	store := NewApprovalStore()
	admitted := store.IngestObserved(ApprovalIngest{
		SessionID: "codex_app_server:s1", LaunchGen: 1, StreamGen: 0,
		Provider: codexAppServerAdapter, Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "codex-1", SessionID: "codex_app_server:s1", AgentKind: codexAppServerAdapter,
				Kind: "approval", Options: codexCertifiedOptions(),
			},
			Provenance:       contract.ProvenanceProviderProtocol,
			Actionable:       true,
			RequiredPerm:     claudeRequiredPerm,
			DeliveryMaterial: codexDeliveryMaterialFor("7"),
		}},
	})
	if !admitted {
		t.Fatal("accepted Codex actionable admission must be unchanged")
	}
}

// ── §6 RuntimeOf resolution and invalidation ──

// driveDeferredExit joins the probe and lets the initial process exit the
// expected way (the C0D lifecycle: deferred exit preserves the coordinator
// identity AND the pending record — that is the claim window).
func driveDeferredExit(t *testing.T, svc *ManagedClaudeService, launcher *fakeClaudeLauncher, id string) string {
	t.Helper()
	svc.mu.Lock()
	rt := svc.runtimes[id]
	store := rt.approvals
	svc.mu.Unlock()
	body := `{"session_id":"s-exit","tool_use_id":"call_exit","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse"}`
	postHook(t, rt, body)
	launcher.feedLine(deferredStreamJSON("s-exit", "call_exit", "Bash", `{"command":"echo pokitclaudeapprovalprobe"}`))

	// The pump joins asynchronously; wait for the record.
	var aid string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if dtos := store.ListSafe(id); len(dtos) == 1 {
			aid = dtos[0].ID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if aid == "" {
		t.Fatal("deferred join never produced a record")
	}
	launcher.closeStream()
	svc.WaitExited(id)

	// The claim window: the record is STILL pending after the deferred exit.
	snap, ok := store.LookupRecord(id, aid)
	if !ok || snap.State != ApprovalPending {
		t.Fatalf("deferred exit must preserve the pending record: ok=%v state=%v", ok, snap.State)
	}
	return aid
}

func TestClaudeRuntimeOf_LiveAndDeferredExitWindows(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, store, _, runtimeOf := newInstalledClaudeService(t, launcher)
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}

	// Live pre-defer window.
	ref, ok := runtimeOf(id)
	if !ok || ref.LaunchGen != 1 || ref.Adapter != claudeHeadlessAdapter || ref.Version != "2.1.209" || ref.StreamGen != 0 {
		t.Fatalf("live window must resolve: ok=%v ref=%+v", ok, ref)
	}

	// Joined-deferred exit window: identity + pending record survive,
	// RuntimeOf still resolves.
	aid := driveDeferredExit(t, svc, launcher, id)
	ref, ok = runtimeOf(id)
	if !ok || ref.LaunchGen != 1 {
		t.Fatalf("joined-deferred exit window must resolve: ok=%v ref=%+v", ok, ref)
	}

	// Stop revokes the window: identity cleared, record superseded, no
	// resolvable authority remains.
	if err := svc.Stop(id, 1); err != nil {
		t.Fatal(err)
	}
	if _, ok := runtimeOf(id); ok {
		t.Fatal("Stop must invalidate RuntimeOf")
	}
	if snap, ok := store.LookupRecord(id, aid); !ok || snap.State == ApprovalPending {
		t.Fatalf("Stop must revoke the pending record: ok=%v", ok)
	}
	if svc.Coordinator().HasIdentityForRuntime(id, 1) {
		t.Fatal("Stop must clear the coordinator identity")
	}
}

func TestClaudeRuntimeOf_ExitWithoutJoinInvalidates(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, _, _, runtimeOf := newInstalledClaudeService(t, launcher)
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	launcher.closeStream() // exit with no joined deferred
	svc.WaitExited(id)
	if _, ok := runtimeOf(id); ok {
		t.Fatal("exit without joined deferred must invalidate RuntimeOf")
	}
}

func TestClaudeRuntimeOf_KillAndDeleteInvalidate(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, store, _, runtimeOf := newInstalledClaudeService(t, launcher)
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	aid := driveDeferredExit(t, svc, launcher, id)
	if err := svc.Kill(id, 1); err != nil {
		t.Fatal(err)
	}
	if _, ok := runtimeOf(id); ok {
		t.Fatal("Kill must invalidate RuntimeOf")
	}
	if err := svc.Delete(id, 1); err != nil {
		t.Fatal(err)
	}
	if _, ok := runtimeOf(id); ok {
		t.Fatal("Delete must invalidate RuntimeOf")
	}
	if _, ok := store.LookupRecord(id, aid); ok {
		t.Fatal("Delete must clear the store session")
	}
}

func TestClaudeRuntimeOf_TimeoutInvalidates(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, store, _, runtimeOf := newInstalledClaudeService(t, launcher)
	t.Cleanup(func() { launcher.closeStream() })
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	// Live window: joined, process still running.
	aid := driveProbeToPending(t, svc, id, "sess-timeout", "call_timeout")
	if _, ok := runtimeOf(id); !ok {
		t.Fatal("precondition: live window resolves")
	}

	// Advance the clock past the observation timeout and run the stale sweep.
	prevNow := clockNow
	t.Cleanup(func() { clockNow = prevNow })
	base := prevNow()
	clockNow = func() time.Time { return base.Add(claudeObservationTimeout + time.Minute) }
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()
	rt.clearStaleObservations()

	if _, ok := runtimeOf(id); !ok {
		t.Fatal("live window still resolves while the process is current (§6 condition 4)")
	}
	if svc.Coordinator().HasIdentityForRuntime(id, 1) {
		t.Fatal("timeout must clear the coordinator identity")
	}
	if snap, ok := store.LookupRecord(id, aid); !ok || snap.State == ApprovalPending {
		t.Fatalf("timeout must invalidate the pending record: ok=%v", ok)
	}
	// The invalidated record can never be claimed.
	claim := store.ClaimForExecution(ClaimRequest{
		SessionID: id, ApprovalID: aid, OptionID: "allow_once",
		Runtime:        RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0},
		Requester:      RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt", Permissions: []string{claudeRequiredPerm}},
		IdempotencyKey: "timeout.k",
	})
	if claim.Outcome == ClaimGranted || claim.Outcome == ClaimAlreadyAccepted {
		t.Fatalf("timed-out record must not be claimable, got %s", claim.Outcome)
	}
	// Once the process exits, no identity remains and RuntimeOf goes false.
	launcher.closeStream()
	svc.WaitExited(id)
	if _, ok := runtimeOf(id); ok {
		t.Fatal("post-timeout exit must leave no resolvable authority")
	}
}

// In the deferred-exit claim window the store expiry bounds the window: an
// unclaimed record expires and the claim fails closed.
func TestClaudeRuntimeOf_DeferredWindowBoundedByStoreExpiry(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, store, _, _ := newInstalledClaudeService(t, launcher)
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	aid := driveDeferredExit(t, svc, launcher, id)

	// Past the store expiry the claim fails closed even though the
	// coordinator identity still exists until explicit lifecycle cleanup.
	prevNow := store.now
	store.now = func() time.Time { return prevNow().Add(authApprovalExpiry + time.Minute) }
	t.Cleanup(func() { store.now = prevNow })
	claim := store.ClaimForExecution(ClaimRequest{
		SessionID: id, ApprovalID: aid, OptionID: "allow_once",
		Runtime:        RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0},
		Requester:      RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt", Permissions: []string{claudeRequiredPerm}},
		IdempotencyKey: "expiry.k",
	})
	if claim.Outcome != ClaimExpired {
		t.Fatalf("expired deferred-window claim must fail closed, got %s", claim.Outcome)
	}
}

func TestClaudeRuntimeOf_DaemonRestartRestoresNothing(t *testing.T) {
	// A restart is a fresh process: new store, new service, new coordinator.
	svc, store, _, runtimeOf := newInstalledClaudeService(t, &fakeClaudeLauncher{})
	if _, ok := runtimeOf("claude_headless:claude-old"); ok {
		t.Fatal("a fresh daemon must restore no resolvable authority")
	}
	if len(store.ListSafe("claude_headless:claude-old")) != 0 {
		t.Fatal("a fresh daemon must restore no records")
	}
	_ = svc
}

// Installed service + uncertified platform: the create itself fails closed,
// so an uncertified incarnation can never exist under an installed service
// (§6 condition 5 is enforced at the spawn boundary).
func TestClaudeCreate_UncertifiedPlatformFailsClosedWhenInstalled(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, store, _, _ := newInstalledClaudeService(t, launcher)

	prevOS := launchOS
	t.Cleanup(func() { launchOS = prevOS })
	launchOS = "linux"

	if _, err := svc.CreateDetached("/tmp"); err == nil || !strings.Contains(err.Error(), "launch certification") {
		t.Fatalf("installed+uncertified create must fail closed, got %v", err)
	}
	if store.Len() != 0 {
		t.Fatal("failed create must roll back the reserved store slot")
	}
}

// ── §5/§6 dispatch: cross-provider rejection before any provider write ──

type recordingDelivery struct {
	mu    sync.Mutex
	calls []ApprovalDeliveryRequest
	out   DeliveryOutcome
}

func (r *recordingDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	r.mu.Unlock()
	return DeliveryReceipt{Outcome: r.out, ClaimToken: req.ClaimToken, Binding: req.Binding}
}

func (r *recordingDelivery) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func TestDispatch_RoutesOnlyExactAdapter(t *testing.T) {
	codex := &recordingDelivery{out: DeliveryAccepted}
	claude := &recordingDelivery{out: DeliveryAccepted}
	gate := NewGatedApprovalDelivery(NewRuntimeDeliveryGate()) // capacity-0: unavailable
	// Production shape: Claude occupies the Codex dispatcher's fallback slot.
	dispatch := NewDispatchingApprovalDelivery(codex, NewClaudeDispatchingApprovalDelivery(claude, gate))

	mkReq := func(adapter string) ApprovalDeliveryRequest {
		return ApprovalDeliveryRequest{
			ClaimToken: strings.Repeat("a", 32),
			Binding: ApprovalExecutionBinding{
				ApprovalID: "a1", SessionID: adapter + ":s1",
				Runtime:        RuntimeRef{Adapter: adapter, Version: "1", LaunchGen: 1},
				ActionDigest:   strings.Repeat("b", 64),
				PayloadDigest:  strings.Repeat("c", 64),
				IdempotencyKey: "k",
			},
		}
	}

	if r := dispatch.Deliver(mkReq(codexAppServerAdapter)); r.Outcome != DeliveryAccepted {
		t.Fatalf("codex binding: %s", r.Outcome)
	}
	if r := dispatch.Deliver(mkReq(claudeHeadlessAdapter)); r.Outcome != DeliveryAccepted {
		t.Fatalf("claude binding: %s", r.Outcome)
	}
	if r := dispatch.Deliver(mkReq("tmux")); r.Outcome != DeliveryUnavailable {
		t.Fatalf("unknown adapter must terminate at the capacity-0 gate: %s", r.Outcome)
	}
	if codex.count() != 1 || claude.count() != 1 {
		t.Fatalf("exact routing: codex=%d claude=%d", codex.count(), claude.count())
	}
}

// The REAL Claude boundary rejects a cross-provider binding before any
// provider work: no coordinator entry, no resume spawn.
func TestClaudeDelivery_CrossProviderRejectedBeforeWrite(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, _, delivery, _ := newInstalledClaudeService(t, launcher)

	req := ApprovalDeliveryRequest{
		ClaimToken: strings.Repeat("a", 32),
		Binding: ApprovalExecutionBinding{
			ApprovalID: "codex-forged", SessionID: "codex_app_server:s1",
			Runtime:        RuntimeRef{Adapter: codexAppServerAdapter, Version: "0.144.1", LaunchGen: 1},
			ActionDigest:   strings.Repeat("b", 64),
			PayloadDigest:  strings.Repeat("c", 64),
			IdempotencyKey: "k",
		},
		Payload: claudeHookResponseBytes("allow"),
	}
	if r := delivery.Deliver(req); r.Outcome != DeliveryRuntimeMismatch {
		t.Fatalf("cross-provider binding must be runtime_mismatch, got %s", r.Outcome)
	}
	if svc.Coordinator().EntryCount() != 0 {
		t.Fatal("cross-provider rejection must precede any coordinator reservation")
	}
	if launcher.launched {
		t.Fatal("cross-provider rejection must not spawn anything")
	}
}

// A stale-runtime substitution (wrong LaunchGen for a live identity) fails
// at ReserveEntry — before any resume spawn or provider write.
func TestClaudeDelivery_StaleRuntimeSubstitutionRejectedBeforeWrite(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, _, delivery, _ := newInstalledClaudeService(t, launcher)

	rtRef := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	sid := "claude_headless:claude-stale"
	dgst := CanonicalDigest([]byte(`{"command":"echo pokitclaudeapprovalprobe"}`))
	if !svc.Coordinator().ReserveIdentity("claude-stale-a", "cs", "tu", "Bash", dgst, activationCatalogID, sid, rtRef) {
		t.Fatal("ReserveIdentity")
	}
	staleReq := ApprovalDeliveryRequest{
		ClaimToken: strings.Repeat("d", 32),
		Binding: ApprovalExecutionBinding{
			ApprovalID: "claude-stale-a", SessionID: sid,
			Runtime:        RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 2, StreamGen: 0},
			ActionDigest:   strings.Repeat("b", 64),
			PayloadDigest:  payloadDigest(claudeHookResponseBytes("allow")),
			IdempotencyKey: "k",
			OptionID:       "allow_once",
			DeliverySchema: claudeDecisionSchemaV1,
		},
		Payload: claudeHookResponseBytes("allow"),
	}
	if r := delivery.Deliver(staleReq); r.Outcome != DeliveryUnavailable {
		t.Fatalf("stale-runtime substitution must fail before provider write, got %s", r.Outcome)
	}
	if svc.Coordinator().EntryCount() != 0 {
		t.Fatal("stale substitution must leave no reserved entry")
	}
	if launcher.launched {
		t.Fatal("stale substitution must not spawn a resume")
	}
}

func TestCombinedRuntimeResolver_BoundaryDispatch(t *testing.T) {
	codexRef := RuntimeRef{Adapter: codexAppServerAdapter, Version: "0.144.1", LaunchGen: 3}
	claudeRef := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 7}
	codexRO := func(string) (RuntimeRef, bool) { return codexRef, true }
	claudeRO := func(string) (RuntimeRef, bool) { return claudeRef, true }

	both := NewCombinedRuntimeResolver(codexRO, claudeRO)
	if ref, ok := both("codex_app_server:s1"); !ok || ref != codexRef {
		t.Fatalf("both: codex session: %v %v", ref, ok)
	}
	if ref, ok := both("claude_headless:s1"); !ok || ref != claudeRef {
		t.Fatalf("both: claude session: %v %v", ref, ok)
	}
	if _, ok := both("tmux:s1"); ok {
		t.Fatal("both: unknown adapter must not resolve")
	}

	codexOnly := NewCombinedRuntimeResolver(codexRO, nil)
	if ref, ok := codexOnly("codex_app_server:s1"); !ok || ref != codexRef {
		t.Fatalf("codex-only: %v %v", ref, ok)
	}
	if _, ok := codexOnly("claude_headless:s1"); ok {
		t.Fatal("codex-only: claude session must not resolve")
	}

	claudeOnly := NewCombinedRuntimeResolver(nil, claudeRO)
	if _, ok := claudeOnly("codex_app_server:s1"); ok {
		t.Fatal("claude-only: codex session must not resolve")
	}
	if ref, ok := claudeOnly("claude_headless:s1"); !ok || ref != claudeRef {
		t.Fatalf("claude-only: %v %v", ref, ok)
	}
}

// ── §12 launch certification ──

func TestClaudeCreate_LaunchCertificationBoundToRecord(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc, _, _, _ := newInstalledClaudeService(t, launcher)
	t.Cleanup(func() { launcher.closeStream() })
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := svc.Registry().Get(id)
	if !ok {
		t.Fatal("record missing")
	}
	if rec.AttestorKind != claudeLaunchAttestorKind || rec.CertResult != claudeCertCertified || rec.CertReason != "" {
		t.Fatalf("record certification binding: %+v", rec)
	}
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()
	cert := rt.LaunchCertification()
	if cert.PokitSessionID != id || cert.Epoch != 1 || cert.ProcessID != rec.ProcessID || cert.Result != claudeCertCertified {
		t.Fatalf("runtime certification tuple: %+v", cert)
	}
	if cert.ArtifactDigest != testDigest || cert.ArtifactVersion != "2.1.209" {
		t.Fatalf("artifact identity: %+v", cert)
	}
}

// Reviewer evidence 1a: the resume incarnation's certification tuple is
// recorded with the delivery attempt's runtime, under its OWN epoch and
// OpaqueID — never the original's.
func TestClaudeResume_CertificationRecordedPerIncarnation(t *testing.T) {
	launcher := &multiLaunchLauncher{}
	svc, _, _, _ := newInstalledClaudeService(t, launcher)
	t.Cleanup(func() {
		for _, p := range launcher.procs {
			p.Kill()
		}
	})
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	rtRef := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	dgst := CanonicalDigest([]byte(`{"command":"echo pokitclaudeapprovalprobe"}`))
	if !svc.Coordinator().ReserveIdentity("claude-res-a", "cs-res", "tu-res", "Bash", dgst, activationCatalogID, id, rtRef) {
		t.Fatal("ReserveIdentity")
	}
	binding := ApprovalExecutionBinding{
		ApprovalID: "claude-res-a", SessionID: id, Runtime: rtRef,
		ActionDigest:   strings.Repeat("b", 64),
		PayloadDigest:  payloadDigest(claudeHookResponseBytes("allow")),
		IdempotencyKey: "k", OptionID: "allow_once", DeliverySchema: claudeDecisionSchemaV1,
	}
	claimToken := strings.Repeat("e", 32)
	handle, ok := svc.Coordinator().ReserveEntry(claimToken, binding)
	if !ok {
		t.Fatal("ReserveEntry")
	}
	ctx := &resumeContext{
		coordinator: svc.Coordinator(), claimToken: handle.ClaimToken, resumeNonce: handle.ResumeNonce,
		originalRuntime: rtRef, pokitSessionID: id, claudeSessionID: "cs-res",
		toolUseID: "tu-res", toolName: "Bash", inputDigest: dgst,
		expectedDecision: "allow", originalCWD: "/tmp",
	}
	rt, err := svc.ResumeForApproval(handle, ctx)
	if err != nil {
		t.Fatalf("ResumeForApproval: %v", err)
	}
	defer rt.terminate()

	cert := rt.LaunchCertification()
	if cert.Result != claudeCertCertified {
		t.Fatalf("resume incarnation must be certified: %+v", cert)
	}
	if cert.Epoch == 1 {
		t.Fatal("resume incarnation must carry its OWN epoch, not the original's")
	}
	if cert.PokitSessionID != id || cert.AttestorKind != claudeLaunchAttestorKind {
		t.Fatalf("resume tuple binding: %+v", cert)
	}
	orig, _ := svc.Registry().Get(id)
	if cert.ProcessID == orig.ProcessID {
		t.Fatal("resume incarnation must carry its OWN launch identity")
	}
}

// Reviewer evidence 1b: an uncertified resume fails closed BEFORE the
// repeated-hook/witness stage — the delivery ends non-success, the entry is
// cancelled, and nothing is spawned into the witness path.
func TestClaudeResume_UncertifiedNeverReachesWitness(t *testing.T) {
	launcher := &multiLaunchLauncher{}
	svc, store, delivery, _ := newInstalledClaudeService(t, launcher)
	t.Cleanup(func() {
		for _, p := range launcher.procs {
			p.Kill()
		}
	})
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	aid := driveProbeToPending(t, svc, id, "cs-uncert", "call_uncert")
	claim := store.ClaimForExecution(ClaimRequest{
		SessionID: id, ApprovalID: aid, OptionID: "allow_once",
		Runtime:        RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0},
		Requester:      RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt", Permissions: []string{claudeRequiredPerm}},
		IdempotencyKey: "uncert.k",
	})
	if claim.Outcome != ClaimGranted {
		t.Fatalf("claim: %s", claim.Outcome)
	}

	// The platform becomes uncertified between claim and delivery.
	prevOS := launchOS
	t.Cleanup(func() { launchOS = prevOS })
	launchOS = "linux"

	initialLaunches := len(launcher.procs)
	receipt := delivery.Deliver(ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload})
	if receipt.Outcome == DeliveryAccepted {
		t.Fatal("uncertified resume must not be accepted")
	}
	if svc.Coordinator().EntryCount() != 0 {
		t.Fatal("uncertified resume must cancel the reserved entry")
	}
	if len(launcher.procs) != initialLaunches+1 {
		// exactly the one killed resume spawn; its process was terminated
		t.Fatalf("unexpected launches: %d -> %d", initialLaunches, len(launcher.procs))
	}
	commit := store.RecordDelivery(receipt)
	if commit.Committed {
		t.Fatal("uncertified resume must not commit")
	}
	if commit.State != ApprovalDeliveryFailed {
		t.Fatalf("state = %s", commit.State)
	}
}

// The certified end-to-end allow path through the composed production
// components (install → real hook ingest → claim → installed delivery →
// witness → commit) is proven in cmd/devremote
// (TestClaudeDelivery_InstalledDeferredExitAllowCommit), which also drives
// the C0D deferred-exit claim window.
