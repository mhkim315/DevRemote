package v3_0_0

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// Adapter is a real, conformance-passing candidate adapter.
// Set failing=true to get a ReadEvents that always degrades (for FailureIsolation tests).
type Adapter struct {
	failing bool
}

// NewFailingAdapter returns an adapter whose ReadEvents always returns degraded.
func NewFailingAdapter() *Adapter { return &Adapter{failing: true} }

var _ contract.AgentAdapter = (*Adapter)(nil)

func (a *Adapter) Descriptor() contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{
		Name: "claude", Provider: "Claude Code",
		ContractVersion:   contract.ContractVersion,
		SupportedVersions: []string{"3.0.0"},
		Capabilities: []contract.AdapterCapability{
			contract.CapEvents, contract.CapStatus,
			contract.CapApprovalDetection, contract.CapIncrementalRead,
		},
	}
}

func (a *Adapter) Detect(_ context.Context, s contract.SessionContext) (contract.AgentIdentity, error) {
	if s.ProcessName == "claude" {
		return contract.AgentIdentity{Kind: "claude", DisplayName: "Claude Code", Confidence: 0.95}, nil
	}
	return contract.AgentIdentity{Kind: "unknown", Confidence: 0.1}, nil
}

func (a *Adapter) DiscoverSessions(_ context.Context, _ contract.DiscoveryInput) ([]contract.DiscoveredSession, error) {
	return nil, nil
}

type fxRec struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	TS   int64  `json:"ts"`
	Text string `json:"text"`
}

func (a *Adapter) NormalizeEvent(_ context.Context, rec contract.RawRecord) (contract.AgentEvent, contract.DegradedInfo) {
	return normalizeFx(rec, "s")
}

func normalizeFx(rec contract.RawRecord, sid string) (contract.AgentEvent, contract.DegradedInfo) {
	var r fxRec
	if err := json.Unmarshal(rec.Bytes, &r); err != nil || r.Kind == "" {
		return contract.SafeEvent(contract.AgentEvent{Type: agent.EventUnknown, Confidence: 0.2}), contract.Degrade("unparseable")
	}
	kinds := map[string]contract.AgentEventType{
		"started":    agent.EventAgentStarted,
		"thinking":   agent.EventThinking,
		"message":    agent.EventAssistantMessage,
		"tool_start": agent.EventToolCallStarted,
		"tool_done":  agent.EventToolCallFinished,
		"approval":   agent.EventApprovalRequested,
		"done":       agent.EventCompleted,
	}
	et := kinds[r.Kind]
	if et == "" { et = agent.EventUnknown }
	conf := 0.9
	if et == agent.EventUnknown { conf = 0.2 }
	id := r.ID
	if id == "" {
		h := sha256.Sum256(rec.Bytes)
		id = hex.EncodeToString(h[:8])
	}
	ev := contract.AgentEvent{
		ID: id, SessionID: sid, AgentKind: "claude",
		Type: et, Seq: r.TS, Confidence: conf,
		Source: rec.Source, Provenance: string(contract.ProvenanceNativeLog),
	}
	if et == agent.EventApprovalRequested { ev.ApprovalID = id }
	return ev, contract.OK()
}

func (a *Adapter) ReadEvents(_ context.Context, in contract.ReadInput) (contract.ReadResult, error) {
	if a.failing {
		return contract.ReadResult{Degraded: contract.Degrade("failing adapter")}, nil
	}
	sid := in.Session.SessionID
	if sid == "" { sid = "s" }
	degraded := false
	var diags []string

	if err := contract.ValidateCursor(in.Cursor); err != nil {
		return contract.ReadResult{Degraded: contract.Degrade("invalid cursor")}, nil
	}
	wm := int64(-1)
	if !in.Cursor.IsEmpty() {
		if w, err := strconv.ParseInt(string(in.Cursor), 10, 64); err == nil {
			wm = w
		}
	}

	records, trunc := contract.BoundBatch(in.Records)
	if trunc { degraded = true }

	limit := contract.EffectiveReadLimit(in.MaxEvents)
	var out []contract.AgentEvent
	seen := map[string]bool{}
	truncOut := false
	for _, rec := range records {
		if !contract.AcceptRecord(rec) { degraded = true; continue }
		ev, _ := normalizeFx(rec, sid)
		if ev.ID == "" || seen[ev.ID] || ev.Seq <= wm { continue }
		if len(out) >= limit { truncOut = true; break }
		seen[ev.ID] = true
		if ev.Seq > wm { wm = ev.Seq }
		out = append(out, ev)
	}
	if truncOut { degraded = true }

	deg := contract.OK()
	if degraded { deg = contract.Degrade("degraded: " + strings.Join(diags, ";")) }
	return contract.ReadResult{Events: out, NextCursor: contract.Cursor(strconv.FormatInt(wm, 10)), Degraded: deg}, nil
}

func (a *Adapter) DetectApproval(_ context.Context, events []contract.AgentEvent) ([]contract.AgentApproval, error) {
	var out []contract.AgentApproval
	for _, e := range events {
		if contract.SafeApprovalGate(e) {
			out = append(out, contract.AgentApproval{
				ID: e.ApprovalID, SessionID: e.SessionID, AgentKind: e.AgentKind,
				Kind: "approval", Status: "pending", Prompt: "approval",
				Source: e.Source, Confidence: e.Confidence, CreatedAt: e.Timestamp,
			})
		}
	}
	return out, nil
}

func (a *Adapter) GetStatus(_ context.Context, in contract.StatusInput) (contract.StatusResult, error) {
	if len(in.Evidence) == 0 {
		return contract.StatusResult{Status: agent.StatusUnknown, Provenance: contract.ProvenanceUnknown, Degraded: contract.Degrade("no evidence")}, nil
	}
	return contract.ResolveStatus(in.Evidence), nil
}
