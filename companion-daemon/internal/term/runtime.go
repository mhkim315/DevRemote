package term

import (
	"context"
	"errors"
	"log"
	"net/http"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/mux"
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
	Verifier          TokenVerifier     // may be nil if auth is not configured
	Events            EventStore        // agent event storage (never nil in production)
	Links             LinkStore         // session link storage (never nil in production)
	Cmds              CommandBroker     // pending command storage (never nil in production)
	Telemetry         *TelemetryService // telemetry state (nil until wired)
	AgentDetector     AgentDetector     // Phase A5: optional agent detector (nil if not wired)
	Approvals         ApprovalStore     // Phase A9: approval tracking (never nil in production)
	InsecureLocalOnly bool              // E6: accepts dev-token in auth middleware
	Activity          *ActivityBuffer   // E8f: terminal activity capture
	Lifecycle         *LifecycleService // M2: Stop/Kill/Delete for managed sessions
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
