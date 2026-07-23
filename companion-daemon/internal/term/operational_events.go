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

// OperationalRuntimeIdentity is the exact provider incarnation to which an
// optional operational sender is bound. It is constructed by the managed
// service after it has committed registration; a runtime receives only the
// sink returned for its own identity.
type OperationalRuntimeIdentity struct {
	Provider         string
	SessionID        string
	RuntimeID        string
	LaunchGeneration int64
}

// OperationalRuntimeBinder is an optional composition-owned extension of the
// neutral sink. When present, it issues a distinct opaque sender for one
// committed runtime. Plain sinks remain valid test/observation seams and are
// used directly.
type OperationalRuntimeBinder interface {
	BindOperationalRuntime(OperationalRuntimeIdentity) OperationalEventSink
}

// OperationalRuntimeRevoker is implemented by an opaque per-runtime sender.
// It takes no caller-supplied identity, so a runtime can revoke only the
// capability already bound to that sender.
type OperationalRuntimeRevoker interface {
	RevokeOperationalRuntime()
}

// bindOperationalRuntime gives a managed runtime its sender. A binder may
// refuse an identity by returning nil; callers must then remain fail-open with
// Timeline observation disabled for that incarnation.
func bindOperationalRuntime(sink OperationalEventSink, identity OperationalRuntimeIdentity) (bound OperationalEventSink) {
	if binder, ok := sink.(OperationalRuntimeBinder); ok {
		defer func() {
			if recover() != nil {
				bound = nil
			}
		}()
		return binder.BindOperationalRuntime(identity)
	}
	return sink
}

func revokeOperationalRuntime(sink OperationalEventSink) {
	defer func() {
		// Observation revocation is best-effort and must never alter the
		// already-authoritative resume/lifecycle outcome.
		_ = recover()
	}()
	if revoker, ok := sink.(OperationalRuntimeRevoker); ok {
		revoker.RevokeOperationalRuntime()
	}
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
