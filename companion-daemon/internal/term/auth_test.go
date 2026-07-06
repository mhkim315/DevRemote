package term

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ── Helpers ──

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

func validClaims(owner, projectRef string) jwt.MapClaims {
	return jwt.MapClaims{
		"iss": "https://" + projectRef + ".supabase.co/auth/v1",
		"sub": owner,
		"aud": "authenticated",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

// jwksServer returns an httptest server serving a valid JWKS response
// containing the given keys, and a pointer to the request count.
func jwksServer(t *testing.T, keys []jwksKeyEntry) (*httptest.Server, *int) {
	t.Helper()
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		resp := struct {
			Keys []jwksKeyEntry `json:"keys"`
		}{Keys: keys}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	return srv, &count
}

type jwksKeyEntry struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

func rsaJWKSEntry(kid string, pub *rsa.PublicKey) jwksKeyEntry {
	return jwksKeyEntry{
		Kid: kid,
		Kty: "RSA",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func ecJWKSEntry(kid string, pub *ecdsa.PublicKey) jwksKeyEntry {
	return jwksKeyEntry{
		Kid: kid,
		Kty: "EC",
		X:   base64.RawURLEncoding.EncodeToString(pub.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(pub.Y.Bytes()),
	}
}

// verifierWithServer creates a verifier pointed at a fake JWKS server.
// SupabaseProjectRef defaults to "test" if not set in cfg.
func verifierWithServer(t *testing.T, cfg AuthConfig, srv *httptest.Server) *SupabaseVerifier {
	t.Helper()
	if cfg.SupabaseProjectRef == "" {
		cfg.SupabaseProjectRef = "test"
	}
	v := NewSupabaseVerifier(cfg)
	v.jwksURL = srv.URL
	v.client = srv.Client()
	return v
}

// ── Basic tests ──

func TestSupabaseVerifier_EmptyTokenProdMode(t *testing.T) {
	t.Parallel()
	v := NewSupabaseVerifier(AuthConfig{InsecureLocalOnly: false, OwnerUUID: "", SupabaseProjectRef: ""})
	if err := v.Verify(context.Background(), "dummy-token"); err == nil {
		t.Error("should fail when config missing in production mode")
	}
	if err := v.Verify(context.Background(), ""); err == nil {
		t.Error("should fail when token is empty in production mode")
	}
}

func TestSupabaseVerifier_EmptyTokenInsecureMode(t *testing.T) {
	t.Parallel()
	v := NewSupabaseVerifier(AuthConfig{InsecureLocalOnly: true})
	if err := v.Verify(context.Background(), ""); err != nil {
		t.Errorf("empty token should pass in insecure mode, got: %v", err)
	}
}

func TestSupabaseVerifier_AuthMiddleware(t *testing.T) {
	t.Parallel()
	v := NewSupabaseVerifier(AuthConfig{InsecureLocalOnly: false, OwnerUUID: "", SupabaseProjectRef: ""})
	h := &Handlers{Verifier: v}
	handler := h.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	req, _ := http.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Errorf("got %d, want 401", rr.Code)
	}
}

func TestSupabaseVerifier_NoVerifierRejectsAll(t *testing.T) {
	t.Parallel()
	h := &Handlers{Verifier: nil}
	handler := h.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	req, _ := http.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Errorf("got %d, want 401", rr.Code)
	}
}

// ── JWT claim validation (using fake JWKS server) ──

func TestSupabaseVerifier_ValidRSAToken(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	srv, reqCount := jwksServer(t, []jwksKeyEntry{rsaJWKSEntry("k1", &key.PublicKey)})
	defer srv.Close()

	token := signJWT(t, validClaims("owner", "test"), jwt.SigningMethodRS256, key, "k1")
	v := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv)

	if err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("valid RSA token rejected: %v", err)
	}
	if *reqCount != 1 {
		t.Errorf("JWKS fetched %d times, want 1", *reqCount)
	}
}

func TestSupabaseVerifier_ValidECDSAToken(t *testing.T) {
	t.Parallel()

	key := mustGenerateECDSAKey()
	srv, reqCount := jwksServer(t, []jwksKeyEntry{ecJWKSEntry("ec1", &key.PublicKey)})
	defer srv.Close()

	token := signJWT(t, validClaims("owner", "test"), jwt.SigningMethodES256, key, "ec1")
	v := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv)

	if err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("valid ECDSA token rejected: %v", err)
	}
	if *reqCount != 1 {
		t.Errorf("JWKS fetched %d times, want 1", *reqCount)
	}
}

func TestSupabaseVerifier_OwnerUUIDMismatch(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	srv, _ := jwksServer(t, []jwksKeyEntry{rsaJWKSEntry("k1", &key.PublicKey)})
	defer srv.Close()

	token := signJWT(t, validClaims("other-owner", "test"), jwt.SigningMethodRS256, key, "k1")
	v := verifierWithServer(t, AuthConfig{OwnerUUID: "my-owner"}, srv)

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected owner mismatch, got nil")
	}
	if err.Error() != "owner mismatch" {
		t.Errorf("got %q, want 'owner mismatch'", err.Error())
	}
}

func TestSupabaseVerifier_IssuerMismatch(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	srv, _ := jwksServer(t, []jwksKeyEntry{rsaJWKSEntry("k1", &key.PublicKey)})
	defer srv.Close()

	claims := validClaims("owner", "test")
	claims["iss"] = "https://wrong.supabase.co/auth/v1"
	token := signJWT(t, claims, jwt.SigningMethodRS256, key, "k1")
	v := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv)

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected issuer mismatch, got nil")
	}
	if err.Error() != "invalid issuer" {
		t.Errorf("got %q, want 'invalid issuer'", err.Error())
	}
}

func TestSupabaseVerifier_AudienceMismatch(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	srv, _ := jwksServer(t, []jwksKeyEntry{rsaJWKSEntry("k1", &key.PublicKey)})
	defer srv.Close()

	claims := validClaims("owner", "test")
	claims["aud"] = "wrong-audience"
	token := signJWT(t, claims, jwt.SigningMethodRS256, key, "k1")
	v := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv)

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected audience mismatch, got nil")
	}
	if err.Error() != "invalid audience" {
		t.Errorf("got %q, want 'invalid audience'", err.Error())
	}
}

func TestSupabaseVerifier_MissingSubject(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	srv, _ := jwksServer(t, []jwksKeyEntry{rsaJWKSEntry("k1", &key.PublicKey)})
	defer srv.Close()

	claims := validClaims("owner", "test")
	delete(claims, "sub")
	token := signJWT(t, claims, jwt.SigningMethodRS256, key, "k1")
	v := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv)

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected missing sub, got nil")
	}
	if err.Error() != "missing sub claim" {
		t.Errorf("got %q, want 'missing sub claim'", err.Error())
	}
}

func TestSupabaseVerifier_UnsupportedSigningMethod(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	srv, _ := jwksServer(t, []jwksKeyEntry{rsaJWKSEntry("k1", &key.PublicKey)})
	defer srv.Close()

	// HS256 is never supported in production mode.
	token := signJWT(t, validClaims("owner", "test"), jwt.SigningMethodHS256, []byte("secret"), "")
	v := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv)

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected unsupported signing method, got nil")
	}
}

// ── JWKS cache tests ──

func TestSupabaseVerifier_JWKSCacheIndependence(t *testing.T) {
	t.Parallel()

	key1 := mustGenerateRSAKey()
	key2 := mustGenerateRSAKey()

	srv1, c1 := jwksServer(t, []jwksKeyEntry{rsaJWKSEntry("k1", &key1.PublicKey)})
	defer srv1.Close()
	srv2, c2 := jwksServer(t, []jwksKeyEntry{rsaJWKSEntry("k2", &key2.PublicKey)})
	defer srv2.Close()

	v1 := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv1)
	v2 := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv2)

	// Verify token against v1 — only srv1 should be hit.
	token1 := signJWT(t, validClaims("owner", "test"), jwt.SigningMethodRS256, key1, "k1")
	if err := v1.Verify(context.Background(), token1); err != nil {
		t.Fatalf("v1: %v", err)
	}
	if *c1 != 1 || *c2 != 0 {
		t.Errorf("c1=%d c2=%d, want c1=1 c2=0", *c1, *c2)
	}

	// Verify token against v2 — only srv2 should be hit.
	token2 := signJWT(t, validClaims("owner", "test"), jwt.SigningMethodRS256, key2, "k2")
	if err := v2.Verify(context.Background(), token2); err != nil {
		t.Fatalf("v2: %v", err)
	}
	if *c2 != 1 {
		t.Errorf("c2=%d, want 1", *c2)
	}
}

func TestSupabaseVerifier_CacheTTLAndFetchCount(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	srv, reqCount := jwksServer(t, []jwksKeyEntry{rsaJWKSEntry("k1", &key.PublicKey)})
	defer srv.Close()

	v := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv)
	// Override TTL to be short.
	v.jwksTTL = 50 * time.Millisecond

	token := signJWT(t, validClaims("owner", "test"), jwt.SigningMethodRS256, key, "k1")

	// First verify — triggers fetch.
	if err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	if *reqCount != 1 {
		t.Fatalf("first fetch count = %d, want 1", *reqCount)
	}
	fc1 := v.FetchCount()
	if fc1 != 1 {
		t.Fatalf("FetchCount = %d, want 1", fc1)
	}

	// Second verify within TTL — should NOT trigger another fetch.
	if err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("second verify: %v", err)
	}
	if *reqCount != 1 {
		t.Errorf("second verify: reqCount = %d, want 1 (cached)", *reqCount)
	}

	// Wait for TTL to expire.
	time.Sleep(100 * time.Millisecond)

	// Third verify after TTL — should trigger a re-fetch.
	if err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("third verify: %v", err)
	}
	if *reqCount != 2 {
		t.Errorf("third verify: reqCount = %d, want 2 (re-fetched)", *reqCount)
	}
	fc3 := v.FetchCount()
	if fc3 != 2 {
		t.Errorf("FetchCount = %d, want 2", fc3)
	}
}

func TestSupabaseVerifier_ContextCancellation(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	srv, _ := jwksServer(t, []jwksKeyEntry{rsaJWKSEntry("k1", &key.PublicKey)})
	defer srv.Close()

	v := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv)
	v.mu.Lock()
	v.expires = time.Time{}
	v.mu.Unlock()

	token := signJWT(t, validClaims("owner", "test"), jwt.SigningMethodRS256, key, "k1")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := v.Verify(ctx, token)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled in chain, got: %v", err)
	}
}

func TestSupabaseVerifier_StaleCacheUsedOnFetchFailure(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	// First JWKS request succeeds; subsequent requests return 500.
	var fetchCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		if fetchCount > 1 {
			w.WriteHeader(500)
			return
		}
		resp := struct {
			Keys []jwksKeyEntry `json:"keys"`
		}{Keys: []jwksKeyEntry{rsaJWKSEntry("k1", &key.PublicKey)}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	v := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv)
	v.jwksTTL = 50 * time.Millisecond

	token := signJWT(t, validClaims("owner", "test"), jwt.SigningMethodRS256, key, "k1")

	// First verify — succeeds, caches key.
	if err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	if fetchCount != 1 {
		t.Fatalf("fetchCount = %d, want 1", fetchCount)
	}

	// Wait for TTL to expire.
	time.Sleep(100 * time.Millisecond)

	// Second verify — TTL expired, re-fetch fails (500), uses stale key.
	if err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("second verify should use stale cache: %v", err)
	}
	if fetchCount != 2 {
		t.Errorf("fetchCount = %d, want 2 (re-fetch attempted)", fetchCount)
	}

	// Verify the stale key is still valid.
	if err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("stale key should still verify: %v", err)
	}
	// No additional fetch because expires was NOT updated after the 500.
	if fetchCount != 3 {
		t.Errorf("fetchCount = %d, want 3 (fetch retried each time on stale)", fetchCount)
	}
}

func TestSupabaseVerifier_StaleCacheDoesNotCreateMissingKey(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKey()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	v := verifierWithServer(t, AuthConfig{OwnerUUID: "owner"}, srv)
	// Pre-populate with key "k1" but token uses "k2".
	v.mu.Lock()
	v.keys = map[string]interface{}{"k1": &key.PublicKey}
	v.expires = time.Time{} // expired → will try to fetch
	v.mu.Unlock()

	token := signJWT(t, validClaims("owner", "test"), jwt.SigningMethodRS256, key, "k2")

	err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for unknown kid after fetch failure, got nil")
	}
}

// ── Options ──

func TestSupabaseVerifier_Options(t *testing.T) {
	t.Parallel()
	customClient := &http.Client{Timeout: 5 * time.Second}
	v := NewSupabaseVerifier(AuthConfig{InsecureLocalOnly: true}, WithHTTPClient(customClient), WithJWKSTTL(30*time.Minute))
	if v.client != customClient {
		t.Error("WithHTTPClient not applied")
	}
	if v.jwksTTL != 30*time.Minute {
		t.Errorf("WithJWKSTTL: got %v", v.jwksTTL)
	}
}

// ── App route verifier injection ──

func TestHandlers_AuthMiddlewareUsesInjectedVerifier(t *testing.T) {
	// fakeVerifier records that it was called and returns a fixed error.
	fv := &fakeVerifier{reject: "test-rejection"}
	h := &Handlers{Verifier: fv}
	handler := h.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if !fv.called {
		t.Error("fake verifier was not called")
	}
	if rr.Code != 401 {
		t.Errorf("got %d, want 401 (rejected by fake verifier)", rr.Code)
	}
}

type fakeVerifier struct {
	called bool
	reject string
}

func (f *fakeVerifier) Verify(ctx context.Context, token string) error {
	f.called = true
	if f.reject != "" {
		return fmt.Errorf("%s", f.reject)
	}
	return nil
}
