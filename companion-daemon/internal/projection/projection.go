// Package projection derives read-only Activity and Transcript views from the
// Timeline writer. It deliberately owns no lifecycle or Transcript authority.
package projection

import (
	"fmt"
	"sort"
	"time"

	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
	"devremote/companion-daemon/internal/transcript"
)

type ActivityItem struct {
	EventID          string
	SessionID        string
	RuntimeID        string
	LaunchGeneration int64
	Provider         string
	EventKind        contract.EventKind
	Summary          string
	OccurredAt       time.Time
	ProjectionOrder  int64
}

// TranscriptItem is the Timeline-side equivalent of the safe fields in a
// TranscriptSegment. Text is intentionally computed from the closed display
// mapping, never copied from a Timeline payload.
type TranscriptItem struct {
	EventID           string
	AgentEventRef     string
	SessionID         string
	AgentKind         string
	EventType         string
	Text              string
	ToolName          string
	RuntimeID         string
	LaunchGeneration  int64
	SourceIncarnation string
	ProjectionOrder   int64
}

// FixtureEpochBinding is test/oracle metadata. EpochOccurrence, rather than
// an envelope field, separates a restored runtime that reuses its tuple.
type FixtureEpochBinding struct {
	EpochOccurrence      uint64
	SessionID            string
	TranscriptGeneration int64
	RuntimeID            string
	LaunchGeneration     int64
	TimelineEventIDs     []string
}

type GapMarker struct {
	SessionID           string
	RuntimeID           string
	LaunchGeneration    int64
	EpochOccurrence     uint64
	SourceIncarnation   string
	GlobalDroppedBefore uint64
	GlobalDroppedAfter  uint64
	DegradedReason      string
	Reason              string
	ProjectionOrder     int64
}

// Snapshot is immutable: every call derives ordering solely from the writer
// read returned for that call. It has no shared projection cursor.
type Snapshot struct {
	Activity        []ActivityItem
	Transcript      []TranscriptItem
	Gaps            []GapMarker
	Stats           writer.Stats
	Degraded        bool
	DegradedReason  string
	Collisions      int
	Unknown         int
	RingOverwritten uint64
	Unexplained     int
}

type Projector struct{ writer *writer.Writer }

func NewProjector(w *writer.Writer) *Projector { return &Projector{writer: w} }

func (p *Projector) Activity() []ActivityItem     { return p.Snapshot(nil).Activity }
func (p *Projector) Transcript() []TranscriptItem { return p.Snapshot(nil).Transcript }

// Snapshot projects a stable Writer.ReadRecent snapshot. Bindings are only
// used to scope explicit loss markers; without an unambiguous fixture scope a
// marker is not guessed and the snapshot is unexplained.
func (p *Projector) Snapshot(bindings []FixtureEpochBinding) Snapshot {
	if p == nil || p.writer == nil {
		return Snapshot{Activity: []ActivityItem{}, Transcript: []TranscriptItem{}}
	}
	envs := p.writer.ReadRecent(1000)
	stats := p.writer.Stats()
	degraded, reason := p.writer.HealthSnapshot()
	out := Snapshot{Activity: make([]ActivityItem, 0, len(envs)), Transcript: make([]TranscriptItem, 0, len(envs)), Stats: stats, Degraded: degraded, DegradedReason: reason}
	seen := make(map[string]string, len(envs))
	for _, env := range envs {
		digest, err := env.CanonicalDigest()
		if err != nil {
			out.Unknown++
			continue
		}
		if old, ok := seen[env.EventID]; ok {
			if old != digest {
				out.Collisions++
			}
			continue // exact replay is idempotent in the projection
		}
		seen[env.EventID] = digest
		if !mapped(env) {
			out.Unknown++
			continue
		}
		order := int64(len(out.Activity))
		out.Activity = append(out.Activity, activity(env, order))
		out.Transcript = append(out.Transcript, transcriptItem(env, order))
	}

	// Appended is per-writer-process. Comparing it to the retained ring makes
	// overwrite observable without falsely calling it Writer.Dropped.
	if stats.Appended > uint64(len(envs)) {
		out.RingOverwritten = stats.Appended - uint64(len(envs))
		if marker, ok := scopedMarker(bindings, "ring_overwrite", stats, reason, int64(len(out.Activity))); ok {
			out.Gaps = append(out.Gaps, marker)
		} else {
			out.Unexplained++
		}
	}
	if degraded && stats.Dropped > 0 {
		if marker, ok := scopedMarker(bindings, "writer_drop", stats, reason, int64(len(out.Activity))); ok {
			out.Gaps = append(out.Gaps, marker)
		} else {
			out.Unexplained++
		}
	}
	return out
}

func scopedMarker(bindings []FixtureEpochBinding, reason string, stats writer.Stats, degradedReason string, order int64) (GapMarker, bool) {
	if len(bindings) != 1 {
		return GapMarker{}, false
	}
	b := bindings[0]
	return GapMarker{SessionID: b.SessionID, RuntimeID: b.RuntimeID, LaunchGeneration: b.LaunchGeneration, EpochOccurrence: b.EpochOccurrence, GlobalDroppedAfter: stats.Dropped, DegradedReason: degradedReason, Reason: reason, ProjectionOrder: order}, true
}

func activity(env contract.Envelope, order int64) ActivityItem {
	return ActivityItem{EventID: env.EventID, SessionID: env.SessionID, RuntimeID: env.RuntimeID, LaunchGeneration: env.LaunchGeneration, Provider: env.Provider, EventKind: env.EventKind, Summary: activitySummary(env), OccurredAt: env.OccurredAt, ProjectionOrder: order}
}

func activitySummary(env contract.Envelope) string {
	if env.Payload.Redacted != nil {
		return env.Payload.Redacted.Summary
	}
	if env.Payload.Digest != nil {
		return "reference:" + env.Payload.Digest.Digest[:min(8, len(env.Payload.Digest.Digest))]
	}
	if env.Payload.Opaque != nil {
		return "reference"
	}
	return ""
}

func transcriptItem(env contract.Envelope, order int64) TranscriptItem {
	t := env.T0Event
	text, tool := displayFor(env)
	return TranscriptItem{EventID: env.EventID, AgentEventRef: t.ID, SessionID: env.SessionID, AgentKind: t.AgentKind, EventType: string(t.Type), Text: text, ToolName: tool, RuntimeID: env.RuntimeID, LaunchGeneration: env.LaunchGeneration, SourceIncarnation: env.SourceIncarnation, ProjectionOrder: order}
}

func displayFor(env contract.Envelope) (string, string) {
	switch env.EventKind {
	case contract.EventProviderInvocationStarted:
		return "Agent started", ""
	case contract.EventProviderInvocationFinished:
		switch string(env.T0Event.Type) {
		case "completed":
			return "Completed", ""
		case "failed":
			return "Failed", ""
		case "interrupted":
			return "Interrupted", ""
		}
	case contract.EventToolCallStarted, contract.EventToolCallFinished:
		return "", truncate(env.T0Event.ToolName, 128)
	case contract.EventApprovalRequested:
		return "Approval requested", ""
	case contract.EventApprovalResolved:
		return "Approval resolved", ""
	case contract.EventStreamObserved:
		return "", ""
	}
	return "", ""
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func mapped(e contract.Envelope) bool {
	t := e.T0Event.Type
	switch e.EventKind {
	case contract.EventProviderInvocationStarted:
		return t == "agent_started"
	case contract.EventProviderInvocationFinished:
		return t == "completed" || t == "failed" || t == "interrupted"
	case contract.EventToolCallStarted:
		return t == "tool_call_started"
	case contract.EventToolCallFinished:
		return t == "tool_call_finished"
	case contract.EventApprovalRequested:
		return t == "approval_requested"
	case contract.EventApprovalResolved:
		return t == "approval_resolved"
	case contract.EventStreamObserved:
		return t == "thinking"
	default:
		return false
	}
}

type EquivalenceReport struct {
	ComparedSessions, TotalComparisons, ExactMatches, ToleratedLosses, ToleratedGaps                        int
	OrderingDivergences, Collisions, Extras, Missings, GenerationMismatches, MisboundApprovals, Unexplained int
	Passed                                                                                                  bool
}

// Compare evaluates the authoritative Transcript response against a Timeline
// snapshot. It never mutates either input and reports every non-mapped fact.
func Compare(response transcript.TranscriptResponse, snap Snapshot, bindings []FixtureEpochBinding) EquivalenceReport {
	r := EquivalenceReport{Collisions: snap.Collisions, Unexplained: snap.Unexplained + snap.Unknown}
	binding, ok := bindingFor(response, bindings)
	if !ok {
		r.GenerationMismatches++
	}
	segments := make([]transcript.TranscriptSegment, 0, len(response.Semantic))
	for _, s := range response.Semantic {
		if s.Kind == transcript.KindAgentEvent && s.Source == transcript.SourceAgentEvent {
			segments = append(segments, s)
		}
	}
	items := snap.Transcript
	if binding != nil {
		filtered := make([]TranscriptItem, 0, len(items))
		for _, item := range items {
			if contains(binding.TimelineEventIDs, item.EventID) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	r.ComparedSessions = boolInt(response.SessionID != "")
	n := len(segments)
	if len(items) < n {
		n = len(items)
	}
	for i := 0; i < n; i++ {
		r.TotalComparisons++
		s, it := segments[i], items[i]
		if s.AgentEventRef == it.AgentEventRef && s.SessionID == it.SessionID && s.AgentKind == it.AgentKind && s.EventType == it.EventType && s.Text == it.Text && s.ToolName == it.ToolName && (binding == nil || contains(binding.TimelineEventIDs, it.EventID)) {
			r.ExactMatches++
		} else {
			if isApproval(it.EventType) && (s.SessionID != it.SessionID || binding == nil) {
				r.MisboundApprovals++
			} else {
				r.Unexplained++
			}
		}
		if i > 0 && (s.Seq <= segments[i-1].Seq || it.ProjectionOrder <= items[i-1].ProjectionOrder) {
			r.OrderingDivergences++
		}
	}
	if len(segments) > len(items) {
		r.Missings += len(segments) - len(items)
	}
	if len(items) > len(segments) {
		r.Extras += len(items) - len(segments)
	}
	for _, gap := range snap.Gaps {
		if binding != nil && gap.EpochOccurrence == binding.EpochOccurrence {
			r.ToleratedGaps++
		} else {
			r.Unexplained++
		}
	}
	r.Passed = r.OrderingDivergences == 0 && r.Collisions == 0 && r.Extras == 0 && r.Missings == 0 && r.GenerationMismatches == 0 && r.MisboundApprovals == 0 && r.Unexplained == 0
	return r
}

func bindingFor(response transcript.TranscriptResponse, bs []FixtureEpochBinding) (*FixtureEpochBinding, bool) {
	for i := range bs {
		if bs[i].SessionID == response.SessionID && bs[i].TranscriptGeneration == response.Generation {
			return &bs[i], true
		}
	}
	return nil, false
}
func contains(a []string, v string) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}
func isApproval(t string) bool { return t == "approval_requested" || t == "approval_resolved" }
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ValidatePairOrder checks matching tool/approval lifecycle ordering without
// using timestamps or shared state. Reference identity is the normalized tool
// name for tools and EventType family for approvals.
func ValidatePairOrder(items []TranscriptItem) error {
	seenTool, seenApproval := map[string]bool{}, false
	for _, it := range items {
		switch it.EventType {
		case "tool_call_started":
			seenTool[it.ToolName] = true
		case "tool_call_finished":
			if !seenTool[it.ToolName] {
				return fmt.Errorf("tool finish before start: %s", it.ToolName)
			}
		case "approval_requested":
			seenApproval = true
		case "approval_resolved":
			if !seenApproval {
				return fmt.Errorf("approval resolution before request")
			}
		}
	}
	return nil
}

// SortedBindings returns an immutable deterministic copy for callers that
// record multiple epoch observations.
func SortedBindings(in []FixtureEpochBinding) []FixtureEpochBinding {
	out := append([]FixtureEpochBinding(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].EpochOccurrence < out[j].EpochOccurrence })
	return out
}
