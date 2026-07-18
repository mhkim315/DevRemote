package term

import (
	"context"
	"errors"
	"log"
	"net/http"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// registryCtxKey is used to store the Registry in a request context.
type registryCtxKey struct{}

// WithRegistry returns a context carrying the Registry.
func WithRegistry(ctx context.Context, reg *mux.Registry) context.Context {
	return context.WithValue(ctx, registryCtxKey{}, reg)
}

// RegistryFromContext extracts the Registry from a context.
var ErrRegistryMissing = errors.New("registry missing from request context")

func RegistryFromContext(ctx context.Context) (*mux.Registry, error) {
	reg, ok := ctx.Value(registryCtxKey{}).(*mux.Registry)
	if !ok || reg == nil {
		return nil, ErrRegistryMissing
	}
	return reg, nil
}

// Handlers groups HTTP handler dependencies so they are visible as struct fields
// rather than hidden behind context extraction or package globals.
type Handlers struct {
	Registry          *mux.Registry
	Verifier          TokenVerifier               // may be nil if auth is not configured
	Events            EventStore                  // agent event storage (never nil in production)
	Cmds              CommandBroker               // pending command storage (never nil in production)
	Telemetry         *TelemetryService           // telemetry state (nil until wired)
	AgentDetector     AgentDetector               // Phase A5: optional agent detector (nil if not wired)
	Approvals         *AuthoritativeApprovalStore // A1: generation-bound approval store (never nil in production)
	InsecureLocalOnly bool                        // E6: accepts dev-token in auth middleware
	Activity          *ActivityBuffer             // E8f: terminal activity capture
	Lifecycle         *LifecycleService           // M2: Stop/Kill/Delete for managed sessions
	// M2.5-4: device authentication (concrete types from devicetrust)
	WSTickets    *devicetrust.WSTicketStore
	ConnRegistry *devicetrust.AuthenticatedConnRegistry
	SessionMgr   *devicetrust.DeviceSessionManager
	HostIdentity *devicetrust.HostIdentity // M2.5-4: for ticket host binding
	Audit        devicetrust.AuditLog      // M2.5-5: minimal local audit (nil ⇒ no audit)
	Transcript   *transcript.Service       // T3: bounded session-isolated Transcript store + projectors
	// A1 remediation: RuntimeOf resolves the CURRENT server-derived runtime identity
	// (adapter/provider version + launch/stream generation) for a session, used by
	// the atomic claim and the pre-delivery runtime revalidation. nil ⇒ no runtime
	// resolver: an actionable claim fails closed. Only actionable approvals reach it;
	// production approvals are non-actionable today, so it is wired when a proven
	// provider action mapping exists.
	RuntimeOf func(sessionID string) (RuntimeRef, bool)
	// ApprovalDelivery is the dedicated daemon-owned approval delivery boundary
	// (never the generic CommandBroker). nil ⇒ the unavailable boundary.
	ApprovalDelivery ApprovalDelivery
	// Catalog is the PA1 read-only managed runtime catalog. It federates
	// Get/List/RuntimeOf across the two accepted provider-owned registries
	// without storing a merged copy or exposing mutation authority.
	// nil ⇒ managed catalog reads return empty (no fallback to legacy).
	Catalog ManagedRuntimeCatalog
	// Managed is the SP0 native managed-session service (nil unless
	// EnableManagedCodex). REST reads managed rows/status DIRECTLY from its
	// owned registry — never through adapter discovery or telemetry.
	Managed *ManagedCodexService
	// ManagedClaude is the C1D native managed Claude session service (nil unless
	// EnableManagedClaude). REST reads managed Claude rows/status DIRECTLY from
	// its owned registry.
	ManagedClaude *ManagedClaudeService
}

// AgentDetector is the agent adapter layer's detection interface.
// Agent events flow through the existing production telemetry path
// (processSession → ResolveAgentLog → ReadNewEvents → EventStore → Events).
type AgentDetector interface {
	DetectAgent(sessionID string, adapterName string, localID string, evidence ProdDetectionEvidence) (agentKind string, agentStatus string, agentConfidence float64)
}

// ProdDetectionEvidence carries product-boundary signals for agent detection.
// Re-exported from agent package to avoid circular imports.
type ProdDetectionEvidence = agent.ProdDetectionEvidence

// AuthMiddleware returns an HTTP middleware that validates JWT tokens using
// the configured TokenVerifier. In insecure local-only mode, accepts a dev token
// for fast local iteration without Supabase auth.
func (h *Handlers) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// E6: insecure local-only mode accepts dev-token without JWT verification.
		if h.InsecureLocalOnly {
			token := ExtractToken(r)
			if token == "dev-token" {
				next(w, r)
				return
			}
		}

		if h.Verifier == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		token := ExtractToken(r)
		if err := h.Verifier.Verify(r.Context(), token); err != nil {
			log.Printf("Auth failed for %s: %v", r.URL.Path, err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
