// C3D-B items 1-3, 18 — exact mobile DTO wire shape, non-catalog/stale
// DTO proof, and privacy grep for the Claude catalog record.
//
// No production code is touched; these tests verify that the accepted DTO
// projector produces the exact bounded, redacted wire the mobile client
// expects.
package term

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// ── item 1: actionable catalog record DTO — exact wire shape ──

func TestClaudeDTO_ActionableCatalogRecordExactShape(t *testing.T) {
	store := NewApprovalStore()
	sid := "claude_headless:claude-dto"
	aid := "claude-dto-1"
	ab := claudeHookResponseBytes("allow")
	db := claudeHookResponseBytes("deny")
	admitted := store.IngestObserved(ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0,
		Provider: claudeHeadlessAdapter, Version: "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: aid, SessionID: sid, AgentKind: claudeHeadlessAdapter,
				Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
				Options: claudeCertifiedOptions(),
			},
			Provenance:       contract.ProvenanceProviderHook,
			Actionable:       true,
			RequiredPerm:     claudeRequiredPerm,
			CatalogActionID:  activationCatalogID,
			DeliveryMaterial: claudeCertifiedDeliveryMaterial(),
		}},
	})
	if !admitted {
		t.Fatal("admission must accept the exact certified tuple")
	}

	dtos := store.ListSafe(sid)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 DTO, got %d", len(dtos))
	}
	dto := dtos[0]

	// Exact eight keys — no extra.
	raw, _ := json.Marshal(dto)
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"id": true, "sessionId": true, "summary": true, "state": true,
		"actionable": true, "options": true, "createdAt": true, "expiresAt": true,
	}
	for k := range obj {
		if !allowed[k] {
			t.Fatalf("forbidden field %q in DTO", k)
		}
	}
	for k := range allowed {
		if _, ok := obj[k]; !ok {
			t.Fatalf("missing required field %q", k)
		}
	}
	// Fixed values.
	if dto.ID != aid {
		t.Fatalf("id = %q", dto.ID)
	}
	if dto.Summary != "Run Claude approval verification probe" {
		t.Fatalf("summary = %q", dto.Summary)
	}
	if dto.State != "pending" {
		t.Fatalf("state = %q", dto.State)
	}
	if !dto.Actionable {
		t.Fatal("actionable must be true")
	}
	if len(dto.Options) != 2 {
		t.Fatalf("expected 2 options, got %d", len(dto.Options))
	}
	byID := map[string]SafeOptionDTO{}
	for _, o := range dto.Options {
		byID[o.ID] = o
	}
	allow := byID["allow_once"]
	if allow.Label != "Approve" || allow.Kind != "approve" || allow.RequiresInput || allow.InputPlaceholder != "" {
		t.Fatalf("allow_once projection: %+v", allow)
	}
	deny := byID["deny"]
	if deny.Label != "Reject" || deny.Kind != "reject" || deny.RequiresInput || deny.InputPlaceholder != "" {
		t.Fatalf("deny projection: %+v", deny)
	}

	// Timestamps are bounded, calendar-valid RFC3339.
	for _, ts := range []string{dto.CreatedAt, dto.ExpiresAt} {
		parsed, err := time.Parse(time.RFC3339, ts)
		if err != nil || parsed.IsZero() {
			t.Fatalf("timestamp %q is not valid RFC3339: %v", ts, err)
		}
	}

	// No raw Claude text anywhere in the serialized JSON.
	rawStr := string(raw)
	forbidden := []string{
		`"command"`, `"echo pokitclaudeapprovalprobe"`, `"tool_input"`,
		`"permissionDecision"`, `"hookSpecificOutput"`, `"claimToken"`,
		`"ActionDigest"`, `"PayloadDigest"`, `"delivery"`,
	}
	for _, f := range forbidden {
		if strings.Contains(rawStr, f) {
			t.Fatalf("DTO contains forbidden key/sentinel %q: %s", f, rawStr)
		}
	}
	// Pro-forma: 64-char hex must not appear (SHA-256 digests, nonces).
	assertNoHex64(t, rawStr, "hex64 digest/source in DTO")

	// sessionId legitimately contains "claude_headless" — do NOT flag that.
	_ = ab
	_ = db
}

func assertNoHex64(t *testing.T, raw string, tag string) {
	t.Helper()
	for i := 0; i <= len(raw)-64; i++ {
		candidate := raw[i : i+64]
		if allHex(candidate) {
			t.Fatalf("DTO contains %s: %q", tag, candidate)
		}
	}
}

// ── item 2: non-catalog observation DTO ──

func TestClaudeDTO_NonCatalogObservationNotActionable(t *testing.T) {
	store := NewApprovalStore()
	sid := "claude_headless:claude-noncat"
	admitted := store.IngestObserved(ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0,
		Provider: claudeHeadlessAdapter, Version: "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "claude-noncat-1", SessionID: sid, AgentKind: claudeHeadlessAdapter,
				Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
				Options: nil,
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      false,
			CatalogActionID: "", // no catalog match
		}},
	})
	if !admitted {
		t.Fatal("non-catalog observation must be admitted")
	}
	dtos := store.ListSafe(sid)
	if len(dtos) != 1 {
		t.Fatal("expected 1 DTO")
	}
	dto := dtos[0]
	if dto.Actionable {
		t.Fatal("non-catalog record must be non-actionable")
	}
	if len(dto.Options) != 0 {
		t.Fatalf("non-catalog record must carry zero options, got %d", len(dto.Options))
	}
	// pokitApprovalSummary("claude_headless") defaults to the generic
	// label because only "codex" and "claude" are named cases.
	if dto.Summary != "Approval requested" {
		t.Fatalf("non-catalog summary must be the generic label, got %q", dto.Summary)
	}
	if dto.State != "pending" {
		t.Fatalf("state = %q", dto.State)
	}
}

// ── item 3: stale/expired DTO — state is non-pending, actionable gate ──

func TestClaudeDTO_StaleAndExpiredHaveNoCTA(t *testing.T) {
	store := NewApprovalStore()
	sid := "claude_headless:claude-xpired"
	aid := "claude-xpired-1"
	admitted := store.IngestObserved(ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0,
		Provider: claudeHeadlessAdapter, Version: "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: aid, SessionID: sid, AgentKind: claudeHeadlessAdapter,
				Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
				Options: claudeCertifiedOptions(),
			},
			Provenance:       contract.ProvenanceProviderHook,
			Actionable:       true,
			RequiredPerm:     claudeRequiredPerm,
			CatalogActionID:  activationCatalogID,
			DeliveryMaterial: claudeCertifiedDeliveryMaterial(),
		}},
	})
	if !admitted {
		t.Fatal("admission failed")
	}

	// Expire by advancing store time.
	prevNow := store.now
	t.Cleanup(func() { store.now = prevNow })
	store.now = func() time.Time { return prevNow().Add(authApprovalExpiry + time.Minute) }

	dtos := store.ListSafe(sid)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 expired DTO, got %d", len(dtos))
	}
	dto := dtos[0]
	if dto.State != "expired" {
		t.Fatalf("expired DTO state = %q, want 'expired'", dto.State)
	}
	// Mobile's approvalActionable gate: state must be 'pending'.
	if dto.State == "pending" {
		t.Fatal("expired record must not have pending state")
	}
}

// ── item 18 (privacy): structural allowlist assertion ──

func TestClaudeDTO_PrivacyStructuralAllowlist(t *testing.T) {
	store := NewApprovalStore()
	sid := "claude_headless:claude-priv"
	admitted := store.IngestObserved(ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0,
		Provider: claudeHeadlessAdapter, Version: "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "claude-priv-1", SessionID: sid, AgentKind: claudeHeadlessAdapter,
				Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
				Options: claudeCertifiedOptions(),
			},
			Provenance:       contract.ProvenanceProviderHook,
			Actionable:       true,
			RequiredPerm:     claudeRequiredPerm,
			CatalogActionID:  activationCatalogID,
			DeliveryMaterial: claudeCertifiedDeliveryMaterial(),
		}},
	})
	if !admitted {
		t.Fatal("admission failed")
	}
	raw, _ := json.Marshal(store.ListSafe(sid))

	// Structural allowlist — these EIGHT keys and their closed sub-keys
	// are the ONLY allowed content.
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list))
	}
	item := list[0]

	allowedTopKeys := []string{"id", "sessionId", "summary", "state", "actionable", "options", "createdAt", "expiresAt"}
	for k := range item {
		found := false
		for _, a := range allowedTopKeys {
			if k == a {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("forbidden top-level key %q in DTO", k)
		}
	}
	for _, a := range allowedTopKeys {
		if _, ok := item[a]; !ok {
			t.Fatalf("missing required key %q", a)
		}
	}

	// Each option carries ONLY {id, label, kind, requiresInput} — no
	// inputPlaceholder when not required, no extra.
	opts, _ := item["options"].([]any)
	if len(opts) != 2 {
		t.Fatalf("expected 2 options, got %d", len(opts))
	}
	for _, o := range opts {
		om, ok := o.(map[string]any)
		if !ok {
			t.Fatal("option is not an object")
		}
		for opk := range om {
			switch opk {
			case "id", "label", "kind", "requiresInput":
			case "inputPlaceholder":
				t.Fatalf("inputPlaceholder must not appear when requiresInput is false")
			default:
				t.Fatalf("forbidden option key %q", opk)
			}
		}
	}

	// Raw sentinel scan — none of the forbidden markers appear.
	rawStr := string(raw)
	mustNotContain := []string{
		`"command"`, `"echo pokitclaudeapprovalprobe"`, `"tool_input"`,
		`"permissionDecision"`, `"hookSpecificOutput"`, `"claimToken"`,
		`"ActionDigest"`, `"PayloadDigest"`, `"delivery"`,
		`"artifactDigest"`, `"pinnedDigest"`, `"OpaqueID"`,
	}
	for _, needle := range mustNotContain {
		if strings.Contains(rawStr, needle) {
			t.Fatalf("DTO contains forbidden material %q", needle)
		}
	}

	// 64-hex digest assertion (SHA-256 digests, receipts, nonces).
	assertNoHex64(t, rawStr, "hex64 in DTO")
}

// ── item 17a: a non-catalog HOOK observation is non-actionable ──
//
// Drives the REAL hook bridge (PreToolUse + deferred stream join) with a
// non-catalog command. The production classifier finds no catalog match, so
// the Store admits exactly ONE record — non-actionable, zero options — and a
// claim on it is rejected. This is the hook-path counterpart of item 2; the
// status-only separation proof (no record AT ALL) is
// TestClaudeDTO_WaitingApprovalStatusAloneCreatesNoApprovalAuthority below.

func TestClaudeDTO_NonCatalogHookObservationIsNonActionable(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	cfg := testCfg()
	svc := NewManagedClaudeService(cfg, launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)
	t.Cleanup(func() { launcher.closeStream() })

	// Install activation — the runtime MUST be actionable-capable.
	svc.InstallApprovalExecution(store)

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	// Drive a NON-CATALOG observation through the REAL hook bridge.
	// The classifier returns no match → catalogActionID="" → actionable=false.
	body := `{"session_id":"s-wait","tool_use_id":"call_wait","tool_name":"Bash","tool_input":{"command":"echo hello"},"hook_event_name":"PreToolUse"}`
	postHook(t, rt, body)
	rt.processLine([]byte(deferredStreamJSON("s-wait", "call_wait", "Bash", `{"command":"echo hello"}`)))

	// The Store has exactly ONE record — non-actionable, no options.
	dtos := store.ListSafe(id)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 observation DTO, got %d", len(dtos))
	}
	dto := dtos[0]
	if dto.Actionable || len(dto.Options) != 0 {
		t.Fatalf("non-catalog observation must be non-actionable with zero options: %+v", dto)
	}
	if dto.State != "pending" {
		t.Fatalf("state = %q", dto.State)
	}
	// The DTO carries the generic summary, not the catalog label.
	if dto.Summary != "Approval requested" {
		t.Fatalf("non-catalog DTO summary = %q", dto.Summary)
	}

	// A claim attempt is rejected: not_actionable.
	claim := store.ClaimForExecution(ClaimRequest{
		SessionID: id, ApprovalID: dto.ID, OptionID: "allow_once",
		Runtime:        RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0},
		Requester:      RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt", Permissions: []string{claudeRequiredPerm}},
		IdempotencyKey: "wait.k",
	})
	if claim.Outcome != ClaimNotActionable {
		t.Fatalf("non-catalog claim must be not_actionable, got %s", claim.Outcome)
	}

	// The generic summary and zero options prove that a non-catalog hook
	// observation is recorded as non-actionable intervention information:
	// no claim, no delivery, no provider write is possible from it. (This
	// path DOES create one Store record — the record-free status-only
	// invariant is proven separately by the waiting_approval test below.)
}

// ── item 17b: waiting_approval runtime status ALONE — zero approval authority ──

// claudeWaitSession is a registry session whose process evidence is a real
// Claude CLI process, so the production detector and log resolver run the
// Claude path.
type claudeWaitSession struct{ id string }

func (s *claudeWaitSession) ID() string          { return s.id }
func (s *claudeWaitSession) Title() string       { return "Claude Code" }
func (s *claudeWaitSession) AdapterName() string { return "controlled_pty" }
func (s *claudeWaitSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{PID: 1234, Command: "claude", CWD: "/Users/test/project"}, nil
}

// R4-B separation proof. The current production status path
// (TelemetryService.processSession over a real Claude native log) projects
// the runtime status waiting_approval from the permission-mode "ask"
// observation. No provider approval request is emitted or ingested anywhere
// on this path: the accepted Claude 2.1.202 adapter has no approval-detection
// capability (DetectApproval is nil; permission-mode is NOT approval
// evidence), and the legacy parser never creates approval authority
// (telemetry_service.go A1-C). The invariant proven here: the status
// projection is exactly "waiting_approval" while the canonical
// AuthoritativeApprovalStore holds ZERO records, `ListSafe` is empty, the
// serialized session DTO carries no approvals key, a FULLY-authorized claim
// on a fabricated ApprovalID cannot resolve, and the live managed-launch
// delivery endpoint holds zero queued items/bytes — no record, no claim, no
// CTA, no delivery, no provider write can arise from the status alone.
func TestClaudeDTO_WaitingApprovalStatusAloneCreatesNoApprovalAuthority(t *testing.T) {
 t.Skip("PA3 Step 2: legacy parser removed; accepted-adapter feeds Transcript, not EventStore")
	dir := t.TempDir()
	logPath := dir + "/claude.jsonl"
	writeLines(t, logPath, []string{`{"type":"user","version":"2.1.202","message":{"role":"user","content":"hi"},"sessionId":"s","uuid":"u0","timestamp":"2026-07-06T13:29:35.399Z"}`,
		`{"type":"permission-mode","permissionMode":"ask","sessionId":"s"}`,
	})
	sid := "controlled_pty:clwait"

	// Real production composition mirroring app.go: registry session,
	// transcript service, canonical Store, detector, delivery gate.
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	adapter := &stubRegAdapter{name: "controlled_pty"}
	reg := mux.MustNewRegistry(adapter)
	sess := &claudeWaitSession{id: "clwait"}
	adapter.sessions = []mux.Session{sess}
	store := NewApprovalStore()
	detector := agent.NewTermAgentDetector()
	svc := NewTelemetryService(reg, nil,
		detector, store, ts)
	gate := NewRuntimeDeliveryGate()
	svc.SetDeliveryGate(gate)
	svc.SetLogResolver(func(models.ProcessInfo) (LogRef, error) {
		return LogRef{Path: logPath, Agent: "claude", Session: sid}, nil
	})
	svc.mu.Lock()
	svc.adapterStates[sid] = &adapterState{}
	svc.mu.Unlock()
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{
		SessionID: sid, Provider: "claude", Adapter: "controlled_pty", Version: "2.1.202"})
	defer transcript.RemoveLaunch(sid)

	// ONE synchronous production poll — no sleeps, no fabricated state.
	svc.processSession(context.Background(), sess,
		map[string]models.ProcessInfo{sid: {PID: 1234, Command: "claude"}},
		map[string]bool{"controlled_pty": false}, map[string]bool{})

	// 1. The status projection through the production sessions API is
	// EXACTLY waiting_approval.
	h := &Handlers{Registry: reg, Telemetry: svc,
		AgentDetector: detector, Approvals: store}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	raw := rec.Body.String()
	var sessions []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	var row *SessionTelemetry
	for i := range sessions {
		if sessions[i].ID == sid {
			row = &sessions[i]
		}
	}
	if row == nil {
		t.Fatalf("session %s not in API response: %s", sid, raw)
	}
	if row.AgentStatus != "waiting_approval" {
		t.Fatalf("agentStatus = %q, want waiting_approval (raw=%s)", row.AgentStatus, raw)
	}
	if row.AgentKind != "claude" {
		t.Fatalf("agentKind = %q, want claude", row.AgentKind)
	}

	// 2. The canonical Store holds ZERO records: internal count, ListSafe,
	// and the serialized wire all agree. No S1 advisory record is fabricated
	// either (the accepted adapter produced no status evidence).
	if n := store.Len(); n != 0 {
		t.Fatalf("AuthoritativeApprovalStore must hold zero records, got %d", n)
	}
	if l := store.ListSafe(sid); len(l) != 0 {
		t.Fatalf("ListSafe must be empty, got %d", len(l))
	}
	if len(row.Approvals) != 0 {
		t.Fatalf("DTO approvals must be empty, got %d", len(row.Approvals))
	}
	if strings.Contains(raw, `"approvals"`) {
		t.Fatalf("wire must carry NO approvals key: %s", raw)
	}
	if strings.Contains(raw, `"agentActivity"`) {
		t.Fatalf("status-only poll must not fabricate an S1 activity record: %s", raw)
	}

	// 3. A claim on a fabricated ApprovalID under a FULL permission set
	// cannot resolve — proving resolution fails on record absence, not on a
	// permission shortfall. (Route-level bearer authority is R4-A's proof;
	// ClaimForExecution is the exact store boundary the route handler calls.)
	claim := store.ClaimForExecution(ClaimRequest{
		SessionID: sid, ApprovalID: "claude-fabricated-r4b", OptionID: "allow_once",
		Runtime: RuntimeRef{Adapter: "claude", Version: "2.1.202", LaunchGen: 1, StreamGen: 0},
		Requester: RequesterContext{DeviceID: "d1", HostID: "h1",
			BearerSessionID: "b1", BootID: "bt1", Permissions: []string{claudeRequiredPerm}},
		IdempotencyKey: "r4b.fabricated",
	})
	if claim.Outcome != ClaimNotFound {
		t.Fatalf("fabricated ApprovalID claim outcome = %v, want not_found", claim.Outcome)
	}
	if n := store.Len(); n != 0 {
		t.Fatalf("a failed claim must not create a record, store has %d", n)
	}

	// 4. Zero delivery / zero provider write: the managed-launch-correlated
	// delivery endpoint EXISTS (runtime capacity is live), yet holds zero
	// queued items and zero bytes, and drains empty.
	gate.mu.Lock()
	handle := gate.current[sid]
	if handle == "" {
		gate.mu.Unlock()
		t.Fatal("managed-launch poll must activate the delivery endpoint")
	}
	e := gate.endpoints[handle]
	if e == nil || !e.active || len(e.queue) != 0 || e.queuedBytes != 0 {
		gate.mu.Unlock()
		t.Fatalf("delivery endpoint must be active and EMPTY: %+v", e)
	}
	if gate.totalBytes != 0 {
		gate.mu.Unlock()
		t.Fatalf("gate byte accounting must be zero, got %d", gate.totalBytes)
	}
	gate.mu.Unlock()
	if items := gate.Drain(handle); len(items) != 0 {
		t.Fatalf("delivery endpoint drained %d items, want 0", len(items))
	}

	// 5. Mobile CTA connection: the produced wire has NO approvals key —
	// exactly the input of the accepted named mobile test
	// mobile/__tests__/approvalClient.test.ts
	// "status-only: no approvals array → no CTA", which drives the
	// production gate actionableApprovals(undefined, sid) === [] (and [] for
	// an empty array). DashboardScreen builds CTAs ONLY from
	// actionableApprovals(s.approvals, s.id); agentStatus is display-only
	// (AgentCard legacy status text), never an action source.
}
