package term

import (
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
)

const (
	maxApprovalsPerSession = 50
	approvalExpiry         = 5 * time.Minute
)

// ApprovalStore tracks pending and recently resolved agent approvals.
type ApprovalStore interface {
	// Upsert merges approvals by ID for a session. Expired approvals are pruned.
	Upsert(sessionID string, approvals []agent.AgentApproval)
	// List returns pending and recently resolved approvals for a session.
	List(sessionID string) []agent.AgentApproval
	// Resolve marks an approval as approved or rejected. Returns false if not found or already resolved.
	Resolve(sessionID, approvalID, status string) bool
}

// NewApprovalStore creates an in-memory ApprovalStore.
func NewApprovalStore() ApprovalStore {
	return &memoryApprovalStore{
		approvals: make(map[string][]agent.AgentApproval),
	}
}

type memoryApprovalStore struct {
	mu        sync.Mutex
	approvals map[string][]agent.AgentApproval
}

func (s *memoryApprovalStore) Upsert(sessionID string, approvals []agent.AgentApproval) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.approvals[sessionID]
	now := time.Now()

	// Merge new approvals by ID, preserving resolved state.
	byID := make(map[string]agent.AgentApproval)
	for _, a := range existing {
		if now.Sub(a.CreatedAt) < approvalExpiry || a.Status != "pending" {
			byID[a.ID] = a
		}
	}
	for _, a := range approvals {
		if existing, ok := byID[a.ID]; ok {
			// Preserve resolved state if already resolved.
			if existing.Status != "pending" {
				continue
			}
		}
		if a.CreatedAt.IsZero() {
			a.CreatedAt = now
		}
		byID[a.ID] = a
	}

	// Rebuild slice, capped.
	result := make([]agent.AgentApproval, 0, len(byID))
	for _, a := range byID {
		result = append(result, a)
	}
	if len(result) > maxApprovalsPerSession {
		result = result[:maxApprovalsPerSession]
	}
	s.approvals[sessionID] = result
}

func (s *memoryApprovalStore) List(sessionID string) []agent.AgentApproval {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.approvals[sessionID]
	if len(existing) == 0 {
		return nil
	}

	now := time.Now()
	result := make([]agent.AgentApproval, 0, len(existing))
	for _, a := range existing {
		// Keep pending + recently resolved (within expiry window).
		if a.Status == "pending" || now.Sub(a.CreatedAt) < approvalExpiry {
			result = append(result, a)
		}
	}
	return result
}

func (s *memoryApprovalStore) Resolve(sessionID, approvalID, status string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.approvals[sessionID]
	for i, a := range existing {
		if a.ID == approvalID {
			if a.Status != "pending" {
				return false // Already resolved.
			}
			now := time.Now()
			existing[i].Status = status
			existing[i].ResolvedAt = &now
			return true
		}
	}
	return false // Not found.
}
