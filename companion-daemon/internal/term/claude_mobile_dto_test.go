// C3D-B items 1-3, 18 — exact mobile DTO wire shape, non-catalog/stale
// DTO proof, and privacy grep for the Claude catalog record.
//
// No production code is touched; these tests verify that the accepted DTO
// projector produces the exact bounded, redacted wire the mobile client
// expects.
package term

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
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
