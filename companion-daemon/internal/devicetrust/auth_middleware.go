package devicetrust

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// principalKey is a private context-key type so external packages cannot
// inject or extract a Principal directly.
type principalKey struct{}

// PrincipalFromContext returns the authenticated Principal, or nil.
func PrincipalFromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

// RequirePrincipal wraps a handler so that the request must carry a valid
// device bearer token. If the caller is authenticated, the Principal is
// injected into the request context. If the caller lacks one or more of
// the required permissions, 403 is returned before the handler runs.
//
//	RequirePrincipal(h, PermSessionsRead)           // one perm
//	RequirePrincipal(h)                             // any authenticated device
func RequirePrincipal(sessions *DeviceSessionManager, next http.HandlerFunc, requiredPerms ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			writeAuthError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		p := sessions.AuthenticateBearer(raw)
		if p == nil {
			writeAuthError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		// Permission check.
		for _, need := range requiredPerms {
			if !hasPerm(p.Permissions, need) {
				writeAuthError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
		}
		ctx := context.WithValue(r.Context(), principalKey{}, p)
		next(w, r.WithContext(ctx))
	}
}

// bearerToken extracts the raw token from an Authorization: Bearer header.
// Returns "" if the header is missing, malformed, or uses a different scheme.
func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return auth[len(prefix):]
}

// hasPerm reports whether perm is in the permissions slice (exact match).
func hasPerm(perms []string, need string) bool {
	for _, p := range perms {
		if p == need {
			return true
		}
	}
	return false
}

// writeAuthError writes a JSON error response with no credential details.
func writeAuthError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
