package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
)

// fixtureAdapter is a minimal, provider-neutral SYNTHETIC adapter used only to
// prove the fixed conformance harness and the contract's safe-default behavior.
// It is NOT a provider adapter and contains no reverse-engineered provider
// formats. Records are tiny synthetic JSON objects: {"id","kind","ts","text"}.
type fixtureAdapter struct {
	failRead bool // when true, ReadEvents returns a typed degraded result (isolation test)
}

var fxBase = time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)

func (fixtureAdapter) Descriptor() AgentAdapterDescriptor {
	return AgentAdapterDescriptor{
		Name:              "fixture",
		Provider:          "Fixture",
		ContractVersion:   ContractVersion,
		SupportedVersions: []string{"synthetic-1"},
		Capabilities:      []AdapterCapability{CapEvents, CapStatus, CapApprovalDetection, CapIncrementalRead},
	}
}

func (fixtureAdapter) Detect(_ context.Context, s SessionContext) (AgentIdentity, error) {
	if s.ProcessName == "fixture-agent" || strings.Contains(s.CWD, "fixture") {
		return AgentIdentity{Kind: "fixture", DisplayName: "Fixture", Confidence: 0.95}, nil
	}
	return AgentIdentity{Kind: "unknown", DisplayName: "Unknown", Confidence: 0.1}, nil
}

func (fixtureAdapter) DiscoverSessions(_ context.Context, in DiscoveryInput) ([]DiscoveredSession, error) {
	// Only a context that names the fixture agent yields a proven correlation;
	// anything else stays unavailable (no invented ownership, no cross-link).
	if in.Session.ProcessName != "fixture-agent" {
		return nil, nil
	}
	return []DiscoveredSession{{
		ProviderSessionID: "prov-" + in.Session.SessionID,
		Provider:          "fixture",
		Correlation:       CorrelationProven,
		Confidence:        0.95,
	}}, nil
}

type fxRecord struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	TS   int    `json:"ts"`
	Text string `json:"text"`
}

func (fixtureAdapter) NormalizeEvent(_ context.Context, rec RawRecord) (AgentEvent, DegradedInfo) {
	var r fxRecord
	if err := json.Unmarshal(rec.Bytes, &r); err != nil || r.Kind == "" {
		// Malformed / unknown → safe unknown event, never a fabricated typed event.
		// Note: the raw bytes (which may hold secrets) are NEVER copied to Text.
		return SafeEvent(AgentEvent{
			ID: hashID(rec.Bytes), SessionID: "s", AgentKind: "fixture",
			Type: agent.EventUnknown, Timestamp: fxBase, Confidence: 0.2, Source: srcOr(rec.Source),
		}), Degrade("unparseable record")
	}
	et := fxKindToType(r.Kind)
	conf := 0.9
	if et == agent.EventUnknown {
		conf = 0.2
	}
	id := r.ID
	if id == "" {
		id = hashID(rec.Bytes)
	}
	return AgentEvent{
		ID: id, SessionID: "s", AgentKind: "fixture",
		Type: et, Timestamp: fxBase.Add(time.Duration(r.TS) * time.Second),
		Text: safeText(r.Text), Confidence: conf, Source: srcOr(rec.Source),
	}, OK()
}

func (a fixtureAdapter) ReadEvents(ctx context.Context, in ReadInput) (ReadResult, error) {
	if a.failRead {
		// Isolation: a failing adapter returns a typed degraded result — no panic,
		// no partial events, and nothing that could affect Live Terminal.
		return ReadResult{Degraded: Degrade("fixture read failure")}, nil
	}
	seen := cursorSet(in.Cursor)
	limit := EffectiveReadLimit(in.MaxEvents)
	var out []AgentEvent
	truncated := false
	for _, rec := range in.Records {
		ev, _ := a.NormalizeEvent(ctx, rec)
		if ev.ID == "" || seen[ev.ID] {
			continue // dedupe by id
		}
		if len(out) >= limit {
			truncated = true
			break
		}
		seen[ev.ID] = true
		out = append(out, ev)
	}
	deg := OK()
	if truncated {
		deg = Degrade("read truncated at bound")
	}
	return ReadResult{Events: out, NextCursor: setCursor(seen), Degraded: deg}, nil
}

func (fixtureAdapter) DetectApproval(_ context.Context, events []AgentEvent) ([]AgentApproval, error) {
	var out []AgentApproval
	for _, e := range events {
		// Default no-approval: only an unambiguous approval_requested at/above the
		// confidence floor qualifies. A near-miss assistant message never does.
		if !SafeApprovalGate(e) {
			continue
		}
		out = append(out, AgentApproval{
			ID: "ap-" + e.ID, SessionID: e.SessionID, AgentKind: e.AgentKind,
			Kind: "approval", Status: "pending", Prompt: "approval requested",
			Source: e.Source, Confidence: e.Confidence, CreatedAt: e.Timestamp,
		})
	}
	return out, nil
}

func (fixtureAdapter) GetStatus(_ context.Context, in StatusInput) (StatusResult, error) {
	if len(in.Evidence) == 0 {
		return StatusResult{Status: agent.StatusUnknown, Provenance: ProvenanceUnknown, Degraded: Degrade("no status evidence")}, nil
	}
	return ResolveStatus(in.Evidence), nil
}

// helpers

func fxKindToType(kind string) AgentEventType {
	switch kind {
	case "started":
		return agent.EventAgentStarted
	case "thinking":
		return agent.EventThinking
	case "message":
		return agent.EventAssistantMessage
	case "tool_start":
		return agent.EventToolCallStarted
	case "tool_done":
		return agent.EventToolCallFinished
	case "approval":
		return agent.EventApprovalRequested
	case "done":
		return agent.EventCompleted
	default:
		return agent.EventUnknown
	}
}

func srcOr(s AgentEventSource) AgentEventSource {
	if IsKnownEventSource(s) {
		return s
	}
	return agent.SourceJSONL
}

// safeText returns a bounded, secret-free rendering of provider text. Provider
// text that still contains a secret/path is dropped rather than surfaced.
func safeText(s string) string {
	if ContainsSensitive(s) {
		return ""
	}
	if len(s) > 256 {
		return s[:256]
	}
	return s
}

func hashID(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

func cursorSet(c Cursor) map[string]bool {
	m := map[string]bool{}
	for _, id := range strings.Split(string(c), "|") {
		if id != "" {
			m[id] = true
		}
	}
	return m
}

func setCursor(seen map[string]bool) Cursor {
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	// Deterministic order for a stable cursor.
	sortStrings(ids)
	return Cursor(strings.Join(ids, "|"))
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// ── Fixtures + harness invocation ──

func fixtureFixtures() ConformanceFixtures {
	rec := func(s string) RawRecord {
		return RawRecord{Bytes: []byte(s), Source: agent.SourceJSONL, Provenance: ProvenanceNativeLog}
	}
	return ConformanceFixtures{
		DetectContext:    SessionContext{SessionID: "s", ProcessName: "fixture-agent", CWD: "/tmp/fixture"},
		ExpectDetectKind: "fixture",
		ValidRecords: []RawRecord{
			rec(`{"id":"e1","kind":"started","ts":1}`),
			rec(`{"id":"e2","kind":"thinking","ts":2}`),
			rec(`{"id":"e3","kind":"tool_start","ts":3}`),
			rec(`{"id":"e4","kind":"done","ts":4}`),
		},
		ExpectTypes: []AgentEventType{agent.EventAgentStarted, agent.EventThinking, agent.EventToolCallStarted, agent.EventCompleted},
		ApprovalRecords: []RawRecord{
			rec(`{"id":"a1","kind":"approval","ts":5}`),
		},
		NearMissRecords: []RawRecord{
			// Looks approval-ish (mentions "approve") but is an assistant message.
			rec(`{"id":"n1","kind":"message","ts":6,"text":"should I approve this?"}`),
		},
		MalformedRecords: []RawRecord{
			rec(`{`),
			rec(`not json at all`),
			{Bytes: nil},
			rec(`{"id":"x","kind":"totally-unknown","ts":7}`),
		},
	}
}

func TestFixtureAdapter_Conformance(t *testing.T) {
	RunAgentContract(t, "fixture", func(t *testing.T) AgentAdapter { return fixtureAdapter{} }, fixtureFixtures())
}

// Strong bound test with DISTINCT records (the generic harness only proves the
// ≤cap invariant with repeated fixtures).
func TestFixtureAdapter_ReadBoundTruncatesAndDegrades(t *testing.T) {
	recs := make([]RawRecord, 0, MaxEventsPerRead+25)
	for i := 0; i < MaxEventsPerRead+25; i++ {
		recs = append(recs, RawRecord{Bytes: []byte(`{"id":"d` + itoa(i) + `","kind":"message","ts":` + itoa(i) + `}`), Source: agent.SourceJSONL})
	}
	res, _ := fixtureAdapter{}.ReadEvents(context.Background(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: recs})
	if len(res.Events) != MaxEventsPerRead {
		t.Fatalf("bounded read returned %d events, want %d", len(res.Events), MaxEventsPerRead)
	}
	if !res.Degraded.Degraded {
		t.Error("truncation must be surfaced as degraded, not silent")
	}
}

// A failing adapter returns a typed degraded result and cannot affect a second,
// independent adapter (proxy for "cannot affect Live Terminal").
func TestFixtureAdapter_FailureIsolation(t *testing.T) {
	failing := fixtureAdapter{failRead: true}
	res, err := failing.ReadEvents(context.Background(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: fixtureFixtures().ValidRecords})
	if err != nil {
		t.Fatalf("failing adapter must not error, it degrades: %v", err)
	}
	if !res.Degraded.Degraded || len(res.Events) != 0 {
		t.Errorf("failing adapter must return degraded + no events, got %+v", res)
	}
	// A healthy adapter is unaffected.
	ok, _ := fixtureAdapter{}.ReadEvents(context.Background(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: fixtureFixtures().ValidRecords})
	if len(ok.Events) == 0 {
		t.Error("healthy adapter affected by a separate failing adapter")
	}
}

// Common DTO snapshot stability: normalized valid events serialize to a stable,
// provider-neutral shape.
func TestFixtureAdapter_DTOSnapshotStable(t *testing.T) {
	res, _ := fixtureAdapter{}.ReadEvents(context.Background(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: fixtureFixtures().ValidRecords})
	got := toJSON(t, res.Events)
	const want = `[{"id":"e1","sessionId":"s","agentKind":"fixture","type":"agent_started","timestamp":"2026-07-12T00:00:01Z","confidence":0.9,"source":"jsonl"},{"id":"e2","sessionId":"s","agentKind":"fixture","type":"thinking","timestamp":"2026-07-12T00:00:02Z","confidence":0.9,"source":"jsonl"},{"id":"e3","sessionId":"s","agentKind":"fixture","type":"tool_call_started","timestamp":"2026-07-12T00:00:03Z","confidence":0.9,"source":"jsonl"},{"id":"e4","sessionId":"s","agentKind":"fixture","type":"completed","timestamp":"2026-07-12T00:00:04Z","confidence":0.9,"source":"jsonl"}]`
	if got != want {
		t.Errorf("DTO snapshot drifted:\n got: %s\nwant: %s", got, want)
	}
}

// A hostile record carrying a secret + absolute path must not surface either in
// the normalized event or diagnostics.
func TestFixtureAdapter_NoSecretLeak(t *testing.T) {
	// Fragmented literal so no credential pattern appears in source.
	body := `{"id":"s1","kind":"message","ts":1,"text":"token ` + "sk" + `-abcd1234567890abcd at /Users/victim/x"}`
	rec := RawRecord{Bytes: []byte(body), Source: agent.SourceJSONL}
	ev, deg := fixtureAdapter{}.NormalizeEvent(context.Background(), rec)
	if ContainsSensitive(ev.Text) {
		t.Errorf("event Text leaked sensitive content: %q", ev.Text)
	}
	if ContainsSensitive(deg.Reason) {
		t.Errorf("degraded reason leaked sensitive content: %q", deg.Reason)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
