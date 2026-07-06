package term

import (
	"context"
	"errors"
	"log"
	"net/http"

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
	Registry *mux.Registry
	Verifier TokenVerifier // may be nil if auth is not configured
	Events   EventStore    // agent event storage (never nil in production)
	Links    LinkStore     // session link storage (never nil in production)
	Cmds     CommandBroker // pending command storage (never nil in production)
}

// AuthMiddleware returns an HTTP middleware that validates JWT tokens using
// the configured TokenVerifier. If no verifier is set, all requests are rejected.
func (h *Handlers) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
