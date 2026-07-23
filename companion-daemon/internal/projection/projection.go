// Package projection derives read-only Activity and Transcript projections
// from Canonical Timeline envelopes in a ring buffer. It owns no authority
// and never modifies the authoritative Transcript service.
package projection

import (
	"time"

	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
)

// ActivityItem is a condensed operational event for display.
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

// TranscriptItem is the Timeline-side comparison output. It mirrors the
// TranscriptSegment structure without calling internal/transcript.
type TranscriptItem struct {
	AgentEventRef    string
	SessionID        string
	AgentKind        string
	EventType        string
	Text             string
	ToolName         string
	RuntimeID        string
	LaunchGeneration int64
	ProjectionOrder  int64
}

// GapMarker is inserted at drop sites so projections expose explicit gaps.
type GapMarker struct {
	SessionID    string
	Generation   int64
	DroppedCount uint64
	OccurredAt   time.Time
}

// Projector reads from a Timeline writer ring buffer and produces projections.
type Projector struct {
	writer *writer.Writer
	order  int64
}

// NewProjector creates a projector backed by the given writer.
func NewProjector(w *writer.Writer) *Projector {
	if w == nil {
		return &Projector{}
	}
	return &Projector{writer: w}
}

// Activity reads recent envelopes and projects them to ActivityItems. If the
// writer is nil, returns nil. If degraded, inserts a GapMarker entry.
func (p *Projector) Activity() []ActivityItem {
	if p.writer == nil {
		return nil
	}
	envelopes := p.writer.ReadRecent(128)
	stats := p.writer.Stats()
	degraded, _ := p.writer.HealthSnapshot()

	items := make([]ActivityItem, 0, len(envelopes))
	for i, env := range envelopes {
		item := ActivityItem{
			EventID:          env.EventID,
			SessionID:        env.SessionID,
			RuntimeID:        env.RuntimeID,
			LaunchGeneration: env.LaunchGeneration,
			Provider:         env.Provider,
			EventKind:        env.EventKind,
			OccurredAt:       env.OccurredAt,
			ProjectionOrder:  p.order + int64(i),
		}
		item.Summary = activitySummary(env)
		items = append(items, item)
	}
	p.order += int64(len(envelopes))

	if degraded && stats.Dropped > 0 {
		last := envelopes[len(envelopes)-1]
		items = append(items, ActivityItem{
			EventID: "gap-" + last.EventID,
			Summary: "degraded",
			ProjectionOrder: p.order,
		})
	}
	return items
}

func activitySummary(env contract.Envelope) string {
	if env.Payload.Redacted != nil {
		return env.Payload.Redacted.Summary
	}
	if env.Payload.Digest != nil {
		return "reference:" + env.Payload.Digest.Digest[:8]
	}
	return ""
}

// Transcript returns Timeline-derived TranscriptItems. The closed mapping
// follows contract §2b.1.
func (p *Projector) Transcript() []TranscriptItem {
	if p.writer == nil {
		return nil
	}
	envelopes := p.writer.ReadRecent(128)
	items := make([]TranscriptItem, 0, len(envelopes))
	for i, env := range envelopes {
		t := env.T0Event
		item := TranscriptItem{
			AgentEventRef:    t.ID,
			SessionID:        env.SessionID,
			AgentKind:        env.Provider,
			EventType:        string(t.Type),
			ToolName:         truncate(t.ToolName, 128),
			RuntimeID:        env.RuntimeID,
			LaunchGeneration: env.LaunchGeneration,
			ProjectionOrder:  p.order + int64(i),
		}
		item.Text = transcriptText(env)
		items = append(items, item)
	}
	p.order += int64(len(envelopes))
	return items
}

func transcriptText(env contract.Envelope) string {
	switch env.EventKind {
	case contract.EventProviderInvocationStarted:
		return "Agent started"
	case contract.EventProviderInvocationFinished:
		switch env.T0Event.Type {
		case "completed":
			return "Completed"
		case "failed":
			return "Failed"
		case "interrupted":
			return "Interrupted"
		default:
			return "Finished"
		}
	case contract.EventApprovalRequested:
		return "Approval requested"
	case contract.EventApprovalResolved:
		return "Approval resolved"
	case contract.EventToolCallStarted, contract.EventToolCallFinished:
		return ""
	case contract.EventStreamObserved:
		return ""
	default:
		return ""
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
