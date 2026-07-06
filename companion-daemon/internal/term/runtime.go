package term

import (
	"context"
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
func RegistryFromContext(ctx context.Context) *mux.Registry {
	reg, _ := ctx.Value(registryCtxKey{}).(*mux.Registry)
	return reg
}

// InjectRegistry wraps an HTTP handler so it receives the Registry via context.
func InjectRegistry(reg *mux.Registry, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r.WithContext(WithRegistry(r.Context(), reg)))
	}
}
