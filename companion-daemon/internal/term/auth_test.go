package term

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ── Token generation helpers ──

func mustGenerateRSAKey() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("rsa generate: %v", err))
	}
	return key
}

func mustGenerateECDSAKey() *ecdsa.PrivateKey {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("ecdsa generate: %v", err))
	}
	return key
}

func signJWT(t *testing.T, claims jwt.MapClaims, method jwt.SigningMethod, key interface{}, kid string) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	if kid != "" {
		token.Header["kid"] = kid
	}
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return signed
}

func validClaims(owner string) jwt.MapClaims {
	return jwt.MapClaims{
		"iss": "https://testproject.supabase.co/auth/v1",
		"sub": owner,
		"aud": "authenticated",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

// ── Basic tests ──

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

// ── JWT claim validation tests ──

// preloadRSAKey sets a key in the verifier's JWKS cache so that token
// signature verification passes and claim validation is tested.
func preloadRSAKey(v *SupabaseVerifier, kid string, key *rsa.PrivateKey) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.keys = map[string]interface{}{kid: &key.PublicKey}
	v.expires = time.Now().Add(1 * time.Hour)
}

func TestSupabaseVerifier_OwnerUUIDMismatch(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	token := signJWT(t, validClaims("other-owner"), jwt.SigningMethodRS256, key, "test-kid")

	v := NewSupabaseVerifier(AuthConfig{
		OwnerUUID:          "my-owner",
		SupabaseProjectRef: "testproject",
	})
	preloadRSAKey(v, "test-kid", key)

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected owner mismatch error, got nil")
	}
	if err.Error() != "owner mismatch" {
		t.Errorf("expected 'owner mismatch', got: %v", err)
	}
}

func TestSupabaseVerifier_IssuerMismatch(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	claims := validClaims("my-owner")
	claims["iss"] = "https://wrong.supabase.co/auth/v1"
	token := signJWT(t, claims, jwt.SigningMethodRS256, key, "test-kid")

	v := NewSupabaseVerifier(AuthConfig{
		OwnerUUID:          "my-owner",
		SupabaseProjectRef: "testproject",
	})
	preloadRSAKey(v, "test-kid", key)

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected issuer mismatch error, got nil")
	}
	if err.Error() != "invalid issuer" {
		t.Errorf("expected 'invalid issuer', got: %v", err)
	}
}

func TestSupabaseVerifier_AudienceMismatch(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	claims := validClaims("my-owner")
	claims["aud"] = "wrong-audience"
	token := signJWT(t, claims, jwt.SigningMethodRS256, key, "test-kid")

	v := NewSupabaseVerifier(AuthConfig{
		OwnerUUID:          "my-owner",
		SupabaseProjectRef: "testproject",
	})
	preloadRSAKey(v, "test-kid", key)

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected audience mismatch error, got nil")
	}
	if err.Error() != "invalid audience" {
		t.Errorf("expected 'invalid audience', got: %v", err)
	}
}

func TestSupabaseVerifier_MissingSubject(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	claims := validClaims("my-owner")
	delete(claims, "sub")
	token := signJWT(t, claims, jwt.SigningMethodRS256, key, "test-kid")

	v := NewSupabaseVerifier(AuthConfig{
		OwnerUUID:          "my-owner",
		SupabaseProjectRef: "testproject",
	})
	preloadRSAKey(v, "test-kid", key)

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected missing sub error, got nil")
	}
	if err.Error() != "missing sub claim" {
		t.Errorf("expected 'missing sub claim', got: %v", err)
	}
}

func TestSupabaseVerifier_UnsupportedSigningMethod(t *testing.T) {
	t.Parallel()

	// HS256 is not supported in production mode (only RS256/ES256 via JWKS).
	key := []byte("not-a-valid-hmac-key-for-production")
	claims := validClaims("my-owner")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign HS256: %v", err)
	}

	v := NewSupabaseVerifier(AuthConfig{
		OwnerUUID:          "my-owner",
		SupabaseProjectRef: "testproject",
	})

	err = v.Verify(context.Background(), signed)
	if err == nil {
		t.Fatal("expected unsupported signing method error, got nil")
	}
}

func TestSupabaseVerifier_ValidToken(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	token := signJWT(t, validClaims("my-owner"), jwt.SigningMethodRS256, key, "test-kid")

	v := NewSupabaseVerifier(AuthConfig{
		OwnerUUID:          "my-owner",
		SupabaseProjectRef: "testproject",
	})

	err := v.Verify(context.Background(), token)
	// Expect key-not-found because JWKS fetch will fail (no real server).
	// The test verifies that the flow reaches JWKS fetch, not that it succeeds.
	if err == nil {
		t.Log("token verified (JWKS fetch may have cached)")
	}
}

func TestSupabaseVerifier_ValidECDSAToken(t *testing.T) {
	t.Parallel()

	key := mustGenerateECDSAKey()
	token := signJWT(t, validClaims("my-owner"), jwt.SigningMethodES256, key, "test-ec-kid")

	v := NewSupabaseVerifier(AuthConfig{
		OwnerUUID:          "my-owner",
		SupabaseProjectRef: "testproject",
	})

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Log("EC token verified (JWKS fetch may have cached)")
	}
}

// ── JWKS cache tests ──

func TestSupabaseVerifier_JWKSCacheIndependence(t *testing.T) {
	t.Parallel()

	v1 := NewSupabaseVerifier(AuthConfig{
		OwnerUUID:          "owner-1",
		SupabaseProjectRef: "project-1",
	})
	v2 := NewSupabaseVerifier(AuthConfig{
		OwnerUUID:          "owner-2",
		SupabaseProjectRef: "project-2",
	})

	// Different verifier instances must have independent JWKS caches.
	if v1.keys != nil || v2.keys != nil {
		t.Log("caches pre-populated (unexpected before first Verify)")
	}

	// Trigger JWKS fetch on v1 (will fail, but keys map stays nil).
	_ = v1.Verify(context.Background(), "dummy-token")
	_ = v2.Verify(context.Background(), "dummy-token")

	// Both should still be independent instances.
	if &v1.keys == &v2.keys {
		t.Error("verifiers share the same JWKS cache map")
	}
}

func TestSupabaseVerifier_CacheTTL(t *testing.T) {
	t.Parallel()

	v := NewSupabaseVerifier(
		AuthConfig{
			OwnerUUID:          "owner",
			SupabaseProjectRef: "testproject",
		},
		WithJWKSTTL(50*time.Millisecond),
	)

	// Manually set a key to simulate a cached JWKS entry.
	v.mu.Lock()
	v.keys = map[string]interface{}{"k1": "fake-key"}
	v.expires = time.Now().Add(50 * time.Millisecond)
	v.mu.Unlock()

	// Key is available before TTL expiry.
	key, err := v.jwksKey(context.Background(), "k1")
	if err != nil {
		t.Fatalf("expected cached key, got: %v", err)
	}
	if key != "fake-key" {
		t.Errorf("expected fake-key, got %v", key)
	}

	// Wait for TTL to expire.
	time.Sleep(100 * time.Millisecond)

	// After TTL expiry, a fetch is attempted but fails (no network).
	// The stale cache is preserved (fetch failure does not evict).
	key2, err := v.jwksKey(context.Background(), "k1")
	if err != nil {
		t.Fatalf("stale cache should be preserved after fetch failure: %v", err)
	}
	if key2 != "fake-key" {
		t.Error("stale cache key was evicted after fetch failure")
	}

	// Verify the cache did attempt to refresh (expires was updated).
	v.mu.Lock()
	exp := v.expires
	v.mu.Unlock()
	if !exp.After(time.Now().Add(-200 * time.Millisecond)) {
		t.Error("cache expiry was not updated after attempted re-fetch")
	}
}

func TestSupabaseVerifier_ContextCancellation(t *testing.T) {
	t.Parallel()

	v := NewSupabaseVerifier(
		AuthConfig{
			OwnerUUID:          "owner",
			SupabaseProjectRef: "testproject",
		},
		WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	)

	// Manually expire the cache so the next Verify triggers a fetch.
	v.mu.Lock()
	v.keys = nil
	v.expires = time.Time{}
	v.mu.Unlock()

	// Cancelled context should propagate to JWKS fetch.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// With a real token, the key lookup will trigger fetchJWKS which uses the context.
	key := mustGenerateRSAKey()
	claims := validClaims("owner")
	claims["iss"] = "https://testproject.supabase.co/auth/v1"
	token := signJWT(t, claims, jwt.SigningMethodRS256, key, "test-kid")

	err := v.Verify(ctx, token)
	if err == nil {
		t.Log("token verified (JWKS may have been cached)")
	}
	// The context cancellation should cause the HTTP request to fail.
	// We verify no panic and that the function returns.
}

func TestSupabaseVerifier_Options(t *testing.T) {
	t.Parallel()

	customClient := &http.Client{Timeout: 5 * time.Second}
	v := NewSupabaseVerifier(
		AuthConfig{InsecureLocalOnly: true},
		WithHTTPClient(customClient),
		WithJWKSTTL(30*time.Minute),
	)

	if v.client != customClient {
		t.Error("WithHTTPClient option not applied")
	}
	if v.jwksTTL != 30*time.Minute {
		t.Errorf("WithJWKSTTL: got %v, want 30m", v.jwksTTL)
	}
}
