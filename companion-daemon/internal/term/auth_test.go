package term

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSupabaseVerifier_EmptyTokenProdMode(t *testing.T) {
	t.Parallel()

	v := NewSupabaseVerifier(AuthConfig{
		InsecureLocalOnly:  false,
		OwnerUUID:          "",
		SupabaseProjectRef: "",
	})

	if err := v.Verify(context.Background(), "dummy-token"); err == nil {
		t.Error("Verify should fail when required config is missing in production mode")
	}

	if err := v.Verify(context.Background(), ""); err == nil {
		t.Error("Verify should fail when token is empty in production mode")
	}
}

func TestSupabaseVerifier_EmptyTokenInsecureMode(t *testing.T) {
	t.Parallel()

	v := NewSupabaseVerifier(AuthConfig{
		InsecureLocalOnly: true,
	})

	if err := v.Verify(context.Background(), ""); err != nil {
		t.Errorf("Verify should pass when token is empty in insecure mode, got: %v", err)
	}
}

func TestSupabaseVerifier_AuthMiddleware(t *testing.T) {
	t.Parallel()

	v := NewSupabaseVerifier(AuthConfig{
		InsecureLocalOnly:  false,
		OwnerUUID:          "",
		SupabaseProjectRef: "",
	})

	h := &Handlers{Verifier: v}
	handler := h.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req, _ := http.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for empty token in prod mode, got %d", rr.Code)
	}
}

func TestSupabaseVerifier_NoVerifierRejectsAll(t *testing.T) {
	t.Parallel()

	h := &Handlers{Verifier: nil}
	handler := h.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req, _ := http.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized when no verifier configured, got %d", rr.Code)
	}
}
