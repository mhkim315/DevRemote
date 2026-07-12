package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
)

// fixtureAdapter is a minimal, provider-neutral SYNTHETIC adapter used only to
// exercise the fixed conformance harness and the contract's safe-default
// behavior. It is NOT a provider adapter and contains no reverse-engineered
// provider formats. Records are tiny synthetic JSON objects: {"id","kind","ts","text"}.
// "ts" doubles as the stable per-session ordering key (Seq). The cursor is a
// bounded high-water-mark (max Seq), never an accumulating id set.
type fixtureAdapter struct {
	failRead bool // when true, ReadEvents returns a typed degraded result (isolation)
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
	if in.Session.ProcessName != "fixture-agent" {
		return nil, nil // no invented ownership / cross-link
	}
	limit := EffectiveDiscoveryLimit(in.Limit)
	out := []DiscoveredSession{{
		ProviderSessionID: "prov-" + in.Session.SessionID,
		Provider:          "fixture",
		Correlation:       CorrelationProven,
		Confidence:        0.95,
	}}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type fxRecord struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	TS   int64  `json:"ts"`
	Text string `json:"text"`
}

func (fixtureAdapter) NormalizeEvent(_ context.Context, rec RawRecord) (AgentEvent, DegradedInfo) {
	prov := ProvenanceNativeLog
	if IsKnownProvenance(rec.Provenance) {
		prov = rec.Provenance
	}
	var r fxRecord
	if err := json.Unmarshal(rec.Bytes, &r); err != nil || r.Kind == "" {
		// Malformed / unknown → safe unknown event, never a fabricated typed event.
		// Raw bytes (which may hold secrets) are NEVER copied to Text.
		return SafeEvent(AgentEvent{
			ID: hashID(rec.Bytes), SessionID: "s", AgentKind: "fixture",
			Type: agent.EventUnknown, Timestamp: fxBase, Confidence: 0.2,
			Source: srcOr(rec.Source), Provenance: string(prov),
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
		Type: et, Seq: r.TS, Timestamp: fxBase.Add(time.Duration(r.TS) * time.Second),
		Text: safeText(r.Text), Confidence: conf,
		Source: srcOr(rec.Source), Provenance: string(prov),
	}, OK()
}

func (a fixtureAdapter) ReadEvents(ctx context.Context, in ReadInput) (ReadResult, error) {
	if a.failRead {
		// Isolation: a failing adapter returns a typed degraded result — no panic,
		// no partial events, nothing that could affect Live Terminal.
		return ReadResult{Degraded: Degrade("fixture read failure")}, nil
	}
	degraded := false
	if err := ValidateCursor(in.Cursor); err != nil {
		return ReadResult{Degraded: Degrade("invalid cursor")}, nil
	}
	records, truncBatch := BoundBatch(in.Records)
	degraded = degraded || truncBatch

	watermark := int64(-1)
	if !in.Cursor.IsEmpty() {
		if w, err := strconv.ParseInt(string(in.Cursor), 10, 64); err == nil {
			watermark = w
		}
	}
	limit := EffectiveReadLimit(in.MaxEvents)
	seenID := map[string]bool{}
	var out []AgentEvent
	truncated := false
	for _, rec := range records {
		if !AcceptRecord(rec) {
			degraded = true
			continue // oversized record skipped
		}
		ev, _ := a.NormalizeEvent(ctx, rec)
		if ev.ID == "" || seenID[ev.ID] || ev.Seq <= watermark {
			continue // dedupe by id + high-water-mark resume
		}
		if len(out) >= limit {
			truncated = true
			break
		}
		seenID[ev.ID] = true
		if ev.Seq > watermark {
			watermark = ev.Seq
		}
		out = append(out, ev)
	}
	degraded = degraded || truncated
	deg := OK()
	if degraded {
		deg = Degrade("read truncated or partially skipped at a bound")
	}
	// Bounded cursor: a single high-water-mark number, never an id set.
	return ReadResult{Events: out, NextCursor: Cursor(strconv.FormatInt(watermark, 10)), Degraded: deg}, nil
}

func (fixtureAdapter) DetectApproval(_ context.Context, events []AgentEvent) ([]AgentApproval, error) {
	var out []AgentApproval
	for _, e := range events {
		// Default no-approval; SafeApprovalGate rejects advisory/unknown/prompt-hint/
		// PTY provenance and low confidence, so a near-miss never becomes an approval.
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

// ── Fixtures + harness invocation ──

func fxRec(s string) RawRecord {
	return RawRecord{Bytes: []byte(s), Source: agent.SourceJSONL, Provenance: ProvenanceNativeLog}
}

func fixtureFixtures() ConformanceFixtures {
	return ConformanceFixtures{
		DetectContext:    SessionContext{SessionID: "s", ProcessName: "fixture-agent", CWD: "/tmp/fixture"},
		ExpectDetectKind: "fixture",
		ValidRecords: []RawRecord{
			fxRec(`{"id":"e1","kind":"started","ts":1}`),
			fxRec(`{"id":"e2","kind":"thinking","ts":2}`),
			fxRec(`{"id":"e3","kind":"tool_start","ts":3}`),
			fxRec(`{"id":"e4","kind":"done","ts":4}`),
		},
		ExpectTypes: []AgentEventType{agent.EventAgentStarted, agent.EventThinking, agent.EventToolCallStarted, agent.EventCompleted},
		// Distinct records for the harness bounds/dedupe/cursor checks. id and ts
		// (Seq) are both distinct per i.
		DistinctRecord: func(i int) RawRecord {
			return fxRec(`{"id":"d` + strconv.Itoa(i) + `","kind":"message","ts":` + strconv.Itoa(i) + `}`)
		},
		FailingFactory: func(t *testing.T) AgentAdapter { return fixtureAdapter{failRead: true} },
		ApprovalRecords: []RawRecord{
			fxRec(`{"id":"a1","kind":"approval","ts":5}`),
		},
		NearMissRecords: []RawRecord{
			fxRec(`{"id":"n1","kind":"message","ts":6,"text":"should I approve this?"}`),
		},
		MalformedRecords: []RawRecord{
			fxRec(`{`),
			fxRec(`not json at all`),
			{Bytes: nil},
			fxRec(`{"id":"x","kind":"totally-unknown","ts":7}`),
		},
	}
}

func TestFixtureAdapter_Conformance(t *testing.T) {
	RunAgentContract(t, "fixture", func(t *testing.T) AgentAdapter { return fixtureAdapter{} }, fixtureFixtures())
}

// Common DTO snapshot stability: normalized valid events serialize to a stable,
// provider-neutral shape (now including seq + provenance).
func TestFixtureAdapter_DTOSnapshotStable(t *testing.T) {
	res, _ := fixtureAdapter{}.ReadEvents(context.Background(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: fixtureFixtures().ValidRecords})
	got := toJSON(t, res.Events)
	const want = `[{"id":"e1","sessionId":"s","agentKind":"fixture","type":"agent_started","seq":1,"timestamp":"2026-07-12T00:00:01Z","confidence":0.9,"source":"jsonl","provenance":"native_log"},` +
		`{"id":"e2","sessionId":"s","agentKind":"fixture","type":"thinking","seq":2,"timestamp":"2026-07-12T00:00:02Z","confidence":0.9,"source":"jsonl","provenance":"native_log"},` +
		`{"id":"e3","sessionId":"s","agentKind":"fixture","type":"tool_call_started","seq":3,"timestamp":"2026-07-12T00:00:03Z","confidence":0.9,"source":"jsonl","provenance":"native_log"},` +
		`{"id":"e4","sessionId":"s","agentKind":"fixture","type":"completed","seq":4,"timestamp":"2026-07-12T00:00:04Z","confidence":0.9,"source":"jsonl","provenance":"native_log"}]`
	if got != want {
		t.Errorf("DTO snapshot drifted:\n got: %s\nwant: %s", got, want)
	}
}

// A hostile record carrying a secret + absolute path must not surface either in
// the normalized event or diagnostics.
func TestFixtureAdapter_NoSecretLeak(t *testing.T) {
	body := `{"id":"s1","kind":"message","ts":1,"text":"token ` + "sk" + `-abcd1234567890abcd at /Users/victim/x"}`
	rec := RawRecord{Bytes: []byte(body), Source: agent.SourceJSONL, Provenance: ProvenanceNativeLog}
	ev, deg := fixtureAdapter{}.NormalizeEvent(context.Background(), rec)
	if ContainsSensitive(ev.Text) {
		t.Errorf("event Text leaked sensitive content: %q", ev.Text)
	}
	if ContainsSensitive(deg.Reason) {
		t.Errorf("degraded reason leaked sensitive content: %q", deg.Reason)
	}
}
