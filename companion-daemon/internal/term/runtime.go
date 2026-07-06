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

// InjectRegistry wraps an HTTP handler so it receives the Registry via context.
func InjectRegistry(reg *mux.Registry, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r.WithContext(WithRegistry(r.Context(), reg)))
	}
}

// requireRegistry extracts the Registry from the request context and writes
// an HTTP 500 error if it is missing. Returns (reg, true) on success.
func requireRegistry(w http.ResponseWriter, r *http.Request) (*mux.Registry, bool) {
	reg, err := RegistryFromContext(r.Context())
	if err != nil {
		log.Printf("request registry unavailable: %v", err)
		http.Error(w, "server configuration error", http.StatusInternalServerError)
		return nil, false
	}
	return reg, true
}
