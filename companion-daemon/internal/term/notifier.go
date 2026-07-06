package term

import "context"

// Notifier sends push notifications for approval prompts.
// Tests can inject a fake implementation.
type Notifier interface {
	// ApprovalRequired is called when an AI agent is waiting for user approval.
	ApprovalRequired(ctx context.Context, sessionID string, message string) error
}

// NoopNotifier discards all approval events.
type NoopNotifier struct{}

func (NoopNotifier) ApprovalRequired(_ context.Context, _ string, _ string) error {
	return nil
}
