package term

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyToken_EmptyProdMode(t *testing.T) {
	oldInsecure := InsecureLocalOnly
	oldOwner := OwnerUUID
	oldProject := SupabaseProjectRef
	defer func() {
		InsecureLocalOnly = oldInsecure
		OwnerUUID = oldOwner
		SupabaseProjectRef = oldProject
	}()

	// Prod mode with missing config should fail closed
	InsecureLocalOnly = false
	OwnerUUID = ""
	SupabaseProjectRef = ""

	if VerifyToken("dummy-token") {
		t.Error("VerifyToken should fail when required config is missing in production mode")
	}

	if VerifyToken("") {
		t.Error("VerifyToken should fail when token is empty in production mode")
	}
}

func TestVerifyToken_EmptyInsecureMode(t *testing.T) {
	oldInsecure := InsecureLocalOnly
	defer func() { InsecureLocalOnly = oldInsecure }()

	// Insecure mode should allow empty token
	InsecureLocalOnly = true
	
	if !VerifyToken("") {
		t.Error("VerifyToken should pass when token is empty in insecure-local-only mode")
	}
}

func TestAuthMiddleware(t *testing.T) {
	oldInsecure := InsecureLocalOnly
	oldOwner := OwnerUUID
	oldProject := SupabaseProjectRef
	defer func() {
		InsecureLocalOnly = oldInsecure
		OwnerUUID = oldOwner
		SupabaseProjectRef = oldProject
	}()

	InsecureLocalOnly = false
	OwnerUUID = ""
	SupabaseProjectRef = ""

	handler := AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req, _ := http.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for empty token in prod mode, got %d", rr.Code)
	}
}
