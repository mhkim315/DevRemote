package term

import "time"

// OperationalEventKind is the closed, provider-neutral vocabulary exposed by
// managed runtimes to an optional composition-owned observer. It is evidence
// only and carries no lifecycle, approval, input, or retry authority.
type OperationalEventKind string

const (
	OperationalProviderInvocationStarted  OperationalEventKind = "provider_invocation_started"
	OperationalProviderInvocationFinished OperationalEventKind = "provider_invocation_finished"
	OperationalToolCallStarted            OperationalEventKind = "tool_call_started"
	OperationalToolCallFinished           OperationalEventKind = "tool_call_finished"
	OperationalApprovalRequested          OperationalEventKind = "approval_requested"
	OperationalApprovalResolved           OperationalEventKind = "approval_resolved"
	OperationalStreamObserved             OperationalEventKind = "stream_observed"
)

// OperationalEvent is a bounded, redacted post-commit observation. Provider
// text, tool input/output, approval material, raw transport bytes, prompts,
// and arbitrary metadata have no field in this type and cannot cross the seam.
type OperationalEvent struct {
	Kind             OperationalEventKind
	Provider         string
	SessionID        string
	RuntimeID        string
	LaunchGeneration int64
	SourceID         string
	SourcePosition   string
	ReferenceID      string
	OccurredAt       time.Time
}

// OperationalEventSink is implemented at composition. SubmitAfterCommit must
// perform at most one bounded, non-blocking enqueue and must never call back
// into a managed runtime. A nil sink preserves the pre-Timeline behavior.
type OperationalEventSink interface {
	SubmitAfterCommit(OperationalEvent)
}

// submitOperationalAfterCommit contains a sink panic at the neutral seam.
// Timeline observation can never alter an already-committed provider outcome.
func submitOperationalAfterCommit(sink OperationalEventSink, event OperationalEvent) {
	if sink == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	sink.SubmitAfterCommit(event)
}
