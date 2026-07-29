package saby

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	expectedAudience   = "ficusin-store-saby-sync"
	expectedIssuer     = "https://token.actions.githubusercontent.com"
	expectedRepository = "avpavlo8/ficusin-store"
	expectedRef        = "refs/heads/main"
	expectedWorkflow   = "avpavlo8/ficusin-store/.github/workflows/saby-catalog-sync.yml@refs/heads/main"
	defaultJWKSURL     = "https://token.actions.githubusercontent.com/.well-known/jwks"
)

type jwtHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
}

type jwtClaims struct {
	Issuer     string          `json:"iss"`
	Audience   json.RawMessage `json:"aud"`
	ExpiresAt  int64           `json:"exp"`
	NotBefore  int64           `json:"nbf"`
	Repository string          `json:"repository"`
	Ref        string          `json:"ref"`
	Workflow   string          `json:"workflow_ref"`
}

type jsonWebKeySet struct {
	Keys []jsonWebKey `json:"keys"`
}

type jsonWebKey struct {
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg"`
	KeyType   string `json:"kty"`
	Modulus   string `json:"n"`
	Exponent  string `json:"e"`
}

type OIDCVerifier struct {
	client    *http.Client
	jwksURL   string
	now       func() time.Time
	mu        sync.Mutex
	cached    jsonWebKeySet
	expiresAt time.Time
}

func NewOIDCVerifier() *OIDCVerifier {
	return &OIDCVerifier{
		client:  &http.Client{Timeout: 10 * time.Second},
		jwksURL: defaultJWKSURL,
		now:     time.Now,
	}
}

func (verifier *OIDCVerifier) Verify(ctx context.Context, token string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return authError("jwt-format", errors.New("invalid GitHub token"))
	}

	var header jwtHeader
	if err := decodeJWTPart(parts[0], &header); err != nil {
		return authError("jwt-format", err)
	}
	if header.Algorithm != "RS256" || header.KeyID == "" {
		return authError("jwt-header", errors.New("unsupported GitHub signature"))
	}

	keys, err := verifier.keys(ctx)
	if err != nil {
		return err
	}
	var selected *jsonWebKey
	for index := range keys.Keys {
		if keys.Keys[index].KeyID == header.KeyID {
			selected = &keys.Keys[index]
			break
		}
	}
	if selected == nil {
		return authError("jwks-kid", errors.New("GitHub signing key not found"))
	}
	publicKey, err := selected.publicKey()
	if err != nil {
		return authError("jwk-import", err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return authError("jwt-format", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
		return authError("jwt-signature", err)
	}

	var claims jwtClaims
	if err := decodeJWTPart(parts[1], &claims); err != nil {
		return authError("jwt-format", err)
	}
	audiences, err := claims.audiences()
	if err != nil {
		return authError("jwt-claims", err)
	}
	now := verifier.now().Unix()
	if claims.Issuer != expectedIssuer ||
		!contains(audiences, expectedAudience) ||
		claims.ExpiresAt <= now ||
		claims.NotBefore > now ||
		claims.Repository != expectedRepository ||
		claims.Ref != expectedRef ||
		claims.Workflow != expectedWorkflow {
		return authError("jwt-claims", errors.New("GitHub Actions cannot run this synchronization"))
	}
	return nil
}

func (verifier *OIDCVerifier) keys(ctx context.Context) (jsonWebKeySet, error) {
	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	if len(verifier.cached.Keys) > 0 && verifier.expiresAt.After(verifier.now()) {
		return verifier.cached, nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, verifier.jwksURL, nil)
	if err != nil {
		return jsonWebKeySet{}, authError("jwks-fetch", err)
	}
	response, err := verifier.client.Do(request)
	if err != nil {
		return jsonWebKeySet{}, authError("jwks-fetch", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return jsonWebKeySet{}, authError("jwks-fetch", fmt.Errorf("GitHub JWKS returned %s", response.Status))
	}

	var keys jsonWebKeySet
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&keys); err != nil {
		return jsonWebKeySet{}, authError("jwks-fetch", err)
	}
	if len(keys.Keys) == 0 {
		return jsonWebKeySet{}, authError("jwks-fetch", errors.New("GitHub JWKS is empty"))
	}
	verifier.cached = keys
	verifier.expiresAt = verifier.now().Add(time.Hour)
	return keys, nil
}

func (key jsonWebKey) publicKey() (*rsa.PublicKey, error) {
	if key.KeyType != "RSA" || (key.Algorithm != "" && key.Algorithm != "RS256") {
		return nil, errors.New("unsupported JWK")
	}
	modulus, err := base64.RawURLEncoding.DecodeString(key.Modulus)
	if err != nil || len(modulus) == 0 {
		return nil, errors.New("invalid JWK modulus")
	}
	exponentBytes, err := base64.RawURLEncoding.DecodeString(key.Exponent)
	if err != nil || len(exponentBytes) == 0 || len(exponentBytes) > 4 {
		return nil, errors.New("invalid JWK exponent")
	}
	exponent := 0
	for _, value := range exponentBytes {
		exponent = exponent<<8 | int(value)
	}
	if exponent < 3 {
		return nil, errors.New("invalid JWK exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponent}, nil
}

func (claims jwtClaims) audiences() ([]string, error) {
	var one string
	if err := json.Unmarshal(claims.Audience, &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(claims.Audience, &many); err != nil {
		return nil, err
	}
	return many, nil
}

func decodeJWTPart(value string, target any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(decoded, target)
}

func authError(code string, err error) *AuthError {
	return &AuthError{Code: code, Err: err}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
