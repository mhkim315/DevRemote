package term

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AuthConfig holds the immutable authentication configuration.
type AuthConfig struct {
	OwnerUUID          string
	SupabaseProjectRef string
	InsecureLocalOnly  bool
}

// TokenVerifier is the interface for JWT token verification.
// Tests can inject a fake implementation.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) error
}

// VerifierOption configures a SupabaseVerifier.
type VerifierOption func(*SupabaseVerifier)

// WithHTTPClient sets the HTTP client used for JWKS fetching.
func WithHTTPClient(client *http.Client) VerifierOption {
	return func(v *SupabaseVerifier) {
		v.client = client
	}
}

// WithJWKSTTL sets the JWKS cache time-to-live.
func WithJWKSTTL(ttl time.Duration) VerifierOption {
	return func(v *SupabaseVerifier) {
		v.jwksTTL = ttl
	}
}

// SupabaseVerifier validates Supabase JWT tokens using RS256/ES256 (JWKS)
// or skips verification in insecure mode. It owns the JWKS cache.
type SupabaseVerifier struct {
	config  AuthConfig
	client  *http.Client
	jwksTTL time.Duration

	// jwksURL overrides the constructed JWKS endpoint (for tests).
	// If empty, the URL is built from SupabaseProjectRef.
	jwksURL string

	mu         sync.Mutex
	keys       map[string]interface{} // kid → crypto.PublicKey
	expires    time.Time
	fetchCount int // number of JWKS network fetch attempts
}

// NewSupabaseVerifier creates a verifier for the given auth configuration.
func NewSupabaseVerifier(cfg AuthConfig, opts ...VerifierOption) *SupabaseVerifier {
	v := &SupabaseVerifier{
		config:  cfg,
		client:  &http.Client{Timeout: 10 * time.Second},
		jwksTTL: 1 * time.Hour,
	}
	for _, o := range opts {
		o(v)
	}
	return v
}

// Verify validates a Supabase JWT token against the configured owner and project.
// The context is propagated to JWKS HTTP requests and can cancel them.
func (v *SupabaseVerifier) Verify(ctx context.Context, tokenString string) error {
	if tokenString == "" {
		if v.config.InsecureLocalOnly {
			log.Println("WARN: empty token allowed due to --insecure-local-only")
			return nil
		}
		return fmt.Errorf("empty token rejected in production mode")
	}

	// Insecure mode: accept any token without signature verification.
	if v.config.InsecureLocalOnly {
		parser := jwt.NewParser()
		token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
		if err != nil {
			log.Printf("JWT parse err (dev): %v", err)
			return fmt.Errorf("JWT parse: %w", err)
		}
		if token != nil {
			sub, _ := token.Claims.(jwt.MapClaims)["sub"]
			log.Printf("WARN: accepted unverified token in insecure mode (sub=%v)", sub)
			return nil
		}
		return fmt.Errorf("JWT parse: empty token")
	}

	// Production mode: fail closed if required config is missing.
	if v.config.OwnerUUID == "" || v.config.SupabaseProjectRef == "" {
		log.Printf("ERR: Missing OwnerUUID or SupabaseProjectRef in production mode")
		return fmt.Errorf("auth configuration incomplete")
	}

	// Production mode: verify signature via Supabase JWKS.
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		alg := token.Header["alg"]
		kid, _ := token.Header["kid"].(string)

		switch token.Method.(type) {
		case *jwt.SigningMethodRSA, *jwt.SigningMethodECDSA:
			return v.jwksKey(ctx, kid)
		default:
			return nil, fmt.Errorf("unsupported signing method: %v", alg)
		}
	}

	token, err := jwt.Parse(tokenString, keyFunc)
	if err != nil {
		log.Printf("JWT parse err: %v", err)
		return fmt.Errorf("JWT parse: %w", err)
	}

	if !token.Valid {
		return fmt.Errorf("token invalid")
	}

	// Check issuer.
	iss, _ := token.Claims.GetIssuer()
	expectedIss := "https://" + v.config.SupabaseProjectRef + ".supabase.co/auth/v1"
	if iss != expectedIss {
		log.Printf("JWT rejected: invalid issuer %q", iss)
		return fmt.Errorf("invalid issuer")
	}

	// Check audience.
	aud, _ := token.Claims.GetAudience()
	validAud := false
	for _, a := range aud {
		if a == "authenticated" {
			validAud = true
			break
		}
	}
	if !validAud {
		log.Printf("JWT rejected: invalid audience %v", aud)
		return fmt.Errorf("invalid audience")
	}

	// Check owner UUID.
	sub, _ := token.Claims.GetSubject()
	if sub == "" {
		log.Printf("JWT rejected: missing sub claim")
		return fmt.Errorf("missing sub claim")
	}
	if sub != v.config.OwnerUUID {
		log.Printf("JWT rejected: sub=%q != owner=%q", sub, v.config.OwnerUUID)
		return fmt.Errorf("owner mismatch")
	}

	return nil
}

// jwksKey returns the public key for a given key ID, fetching the JWKS if needed.
// If a refresh fails but the stale cache has the key, it is returned
// (preserving the existing stale-cache behaviour). Context cancellation
// takes precedence over stale fallback.
func (v *SupabaseVerifier) jwksKey(ctx context.Context, kid string) (interface{}, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	needsFetch := v.keys == nil || time.Now().After(v.expires)
	if !needsFetch {
		if _, ok := v.keys[kid]; !ok {
			needsFetch = true
		}
	}

	if needsFetch {
		if err := v.fetchJWKS(ctx); err != nil {
			// Context cancelled → do not use stale cache.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("jwks fetch: %w", err)
			}
			// Stale fallback: if we have the key cached, keep using it.
			if key, ok := v.keys[kid]; ok {
				log.Printf("jwks: refresh failed (%v), using stale key %q", err, kid)
				return key, nil
			}
			return nil, fmt.Errorf("jwks fetch: %w", err)
		}
	}

	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("key %q not found in JWKS", kid)
}

// FetchCount returns the number of JWKS network fetch attempts (for tests).
func (v *SupabaseVerifier) FetchCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.fetchCount
}

// fetchJWKS downloads and caches the JWKS key set from Supabase.
// Returns context errors wrapped with %w so callers can use errors.Is.
// Must be called with v.mu held.
func (v *SupabaseVerifier) fetchJWKS(ctx context.Context) error {
	url := v.jwksURL
	if url == "" {
		projectRef := v.config.SupabaseProjectRef
		url = fmt.Sprintf("https://%s.supabase.co/auth/v1/.well-known/jwks.json", projectRef)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("jwks request err: %v", err)
		return fmt.Errorf("jwks request: %w", err)
	}
	v.fetchCount++
	resp, err := v.client.Do(req)
	if err != nil {
		log.Printf("jwks fetch err: %v", err)
		return fmt.Errorf("jwks fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("jwks fetch status %d", resp.StatusCode)
		return fmt.Errorf("jwks fetch status %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			X   string `json:"x"`
			Y   string `json:"y"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		log.Printf("jwks decode err: %v", err)
		return fmt.Errorf("jwks decode: %w", err)
	}

	newCache := make(map[string]interface{})
	for _, k := range jwks.Keys {
		if k.Kid == "" {
			continue
		}
		switch k.Kty {
		case "RSA":
			nBytes, err1 := base64.RawURLEncoding.DecodeString(k.N)
			eBytes, err2 := base64.RawURLEncoding.DecodeString(k.E)
			if err1 != nil || err2 != nil || len(eBytes) == 0 {
				continue
			}
			e := int(new(big.Int).SetBytes(eBytes).Int64())
			newCache[k.Kid] = &rsa.PublicKey{
				N: new(big.Int).SetBytes(nBytes),
				E: e,
			}
		case "EC":
			xb, err1 := base64.RawURLEncoding.DecodeString(k.X)
			yb, err2 := base64.RawURLEncoding.DecodeString(k.Y)
			if err1 != nil || err2 != nil {
				continue
			}
			newCache[k.Kid] = &ecdsa.PublicKey{
				Curve: elliptic.P256(),
				X:     new(big.Int).SetBytes(xb),
				Y:     new(big.Int).SetBytes(yb),
			}
		}
	}
	v.keys = newCache
	v.expires = time.Now().Add(v.jwksTTL)
	log.Printf("jwks: loaded %d keys (ttl=%v)", len(v.keys), v.jwksTTL)
	return nil
}
