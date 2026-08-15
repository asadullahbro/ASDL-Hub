package github

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

const (
	GitHubOIDCIssuer  = "https://token.actions.githubusercontent.com"
	jwksURL           = "https://token.actions.githubusercontent.com/.well-known/jwks"
	jwksCacheDuration = 1 * time.Hour
)

// OIDCClaims holds the verified claims extracted from a GitHub Actions OIDC token.
type OIDCClaims struct {
	Issuer      string `json:"iss"`
	Audience    string `json:"aud"`
	Subject     string `json:"sub"`
	Repository  string `json:"repository"` // e.g. "org/repo"
	RepoOwner   string `json:"repository_owner"`
	Workflow    string `json:"workflow"`
	RunID       string `json:"run_id"`
	SHA         string `json:"sha"`
	Ref         string `json:"ref"`
	Environment string `json:"environment"`
	EventName   string `json:"event_name"`
	ExpiresAt   int64  `json:"exp"`
	IssuedAt    int64  `json:"iat"`
}

// jwksCache caches the GitHub JWKS to avoid fetching on every request.
type jwksCache struct {
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

var globalJWKSCache = &jwksCache{}

// OIDCVerifier verifies GitHub Actions OIDC tokens.
type OIDCVerifier struct {
	expectedAudience string
	cache            *jwksCache
	httpClient       *http.Client
}

// NewOIDCVerifier creates a verifier. audience should be your Hub's URL,
// e.g. "https://hub.example.com". GitHub Actions will set this automatically
// when the workflow uses the deploy action with hub: <url>.
func NewOIDCVerifier(audience string) *OIDCVerifier {
	return &OIDCVerifier{
		expectedAudience: audience,
		cache:            globalJWKSCache,
		httpClient:       &http.Client{Timeout: 10 * time.Second},
	}
}

// Verify parses and validates a raw JWT string from GitHub Actions.
// Returns the extracted claims on success.
func (v *OIDCVerifier) Verify(rawToken string) (*OIDCClaims, error) {
	header, payload, err := parseJWT(rawToken)
	if err != nil {
		return nil, fmt.Errorf("malformed token: %w", err)
	}

	// Decode claims first (unverified) to get kid for key lookup.
	kid, ok := header["kid"].(string)
	if !ok || kid == "" {
		return nil, fmt.Errorf("token missing kid header")
	}

	pubKey, err := v.getPublicKey(kid)
	if err != nil {
		return nil, fmt.Errorf("failed to get signing key: %w", err)
	}

	if err := verifyRS256(rawToken, pubKey); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	var claims OIDCClaims
	claimsJSON, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decode claims: %w", err)
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	if err := v.validateClaims(&claims); err != nil {
		return nil, fmt.Errorf("claim validation failed: %w", err)
	}

	return &claims, nil
}

func (v *OIDCVerifier) validateClaims(c *OIDCClaims) error {
	now := time.Now().Unix()

	if c.Issuer != GitHubOIDCIssuer {
		return fmt.Errorf("invalid issuer: %q", c.Issuer)
	}
	if c.Audience != v.expectedAudience {
		return fmt.Errorf("invalid audience: %q (expected %q)", c.Audience, v.expectedAudience)
	}
	if c.ExpiresAt < now {
		return fmt.Errorf("token expired at %d", c.ExpiresAt)
	}
	if c.IssuedAt > now+30 { // 30s clock skew tolerance
		return fmt.Errorf("token issued in the future")
	}
	if c.Repository == "" {
		return fmt.Errorf("token missing repository claim")
	}
	return nil
}

// getPublicKey returns the RSA public key for the given kid,
// fetching from GitHub if the cache is stale.
func (v *OIDCVerifier) getPublicKey(kid string) (*rsa.PublicKey, error) {
	v.cache.mu.RLock()
	if time.Since(v.cache.fetchedAt) < jwksCacheDuration {
		key, ok := v.cache.keys[kid]
		v.cache.mu.RUnlock()
		if ok {
			return key, nil
		}
	} else {
		v.cache.mu.RUnlock()
	}

	// Cache miss or stale — fetch fresh JWKS.
	return v.fetchAndCacheKey(kid)
}

func (v *OIDCVerifier) fetchAndCacheKey(kid string) (*rsa.PublicKey, error) {
	v.cache.mu.Lock()
	defer v.cache.mu.Unlock()

	// Double-check after acquiring write lock.
	if time.Since(v.cache.fetchedAt) < jwksCacheDuration {
		if key, ok := v.cache.keys[kid]; ok {
			return key, nil
		}
	}

	resp, err := v.httpClient.Get(jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Alg != "RS256" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(k.N, k.E)
		if err != nil {
			continue // skip malformed keys
		}
		newKeys[k.Kid] = pub
	}

	v.cache.keys = newKeys
	v.cache.fetchedAt = time.Now()

	key, ok := newKeys[kid]
	if !ok {
		return nil, fmt.Errorf("kid %q not found in GitHub JWKS", kid)
	}
	return key, nil
}

// rsaPublicKeyFromJWK builds an *rsa.PublicKey from JWK n and e fields.
func rsaPublicKeyFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("invalid n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("invalid e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	var eBig big.Int
	eBig.SetBytes(eBytes)

	return &rsa.PublicKey{N: n, E: int(eBig.Int64())}, nil
}

// parseJWT splits a JWT into its three base64url-encoded parts and
// decodes the header. Returns the decoded header map, raw payload part, and any error.
func parseJWT(token string) (map[string]interface{}, string, error) {
	parts := splitJWT(token)
	if len(parts) != 3 {
		return nil, "", fmt.Errorf("expected 3 parts, got %d", len(parts))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, "", fmt.Errorf("invalid header encoding: %w", err)
	}

	var header map[string]interface{}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, "", fmt.Errorf("invalid header JSON: %w", err)
	}

	return header, parts[1], nil
}

func splitJWT(token string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			parts = append(parts, token[start:i])
			start = i + 1
		}
	}
	parts = append(parts, token[start:])
	return parts
}
