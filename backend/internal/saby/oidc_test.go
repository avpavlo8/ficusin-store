package saby

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOIDCVerifierAcceptsExpectedGitHubClaims(t *testing.T) {
	t.Parallel()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	keyID := "test-key"
	jwksServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(jsonWebKeySet{Keys: []jsonWebKey{{
			KeyID: keyID, Algorithm: "RS256", KeyType: "RSA",
			Modulus:  base64.RawURLEncoding.EncodeToString(privateKey.N.Bytes()),
			Exponent: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.E)).Bytes()),
		}}})
	}))
	defer jwksServer.Close()

	verifier := NewOIDCVerifier()
	verifier.jwksURL = jwksServer.URL
	verifier.now = func() time.Time { return now }
	token := signedToken(t, privateKey, keyID, map[string]any{
		"iss": expectedIssuer, "aud": expectedAudience,
		"exp": now.Unix() + 300, "nbf": now.Unix() - 10,
		"repository_id": expectedRepositoryID, "repository_owner_id": expectedOwnerID,
		"repository": expectedRepository, "ref": expectedRef,
		"workflow_ref": expectedWorkflow,
	})

	if err := verifier.Verify(context.Background(), token); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestOIDCVerifierRejectsWrongRepository(t *testing.T) {
	t.Parallel()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	keyID := "test-key"
	jwksServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(jsonWebKeySet{Keys: []jsonWebKey{{
			KeyID: keyID, Algorithm: "RS256", KeyType: "RSA",
			Modulus:  base64.RawURLEncoding.EncodeToString(privateKey.N.Bytes()),
			Exponent: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.E)).Bytes()),
		}}})
	}))
	defer jwksServer.Close()

	verifier := NewOIDCVerifier()
	verifier.jwksURL = jwksServer.URL
	verifier.now = func() time.Time { return now }
	token := signedToken(t, privateKey, keyID, map[string]any{
		"iss": expectedIssuer, "aud": expectedAudience,
		"exp": now.Unix() + 300, "nbf": now.Unix() - 10,
		"repository": "someone/else", "ref": expectedRef,
		"workflow_ref": expectedWorkflow,
	})

	err = verifier.Verify(context.Background(), token)
	authErr, ok := err.(*AuthError)
	if !ok || authErr.Code != "jwt-claims" {
		t.Fatalf("Verify() error = %#v, want jwt-claims", err)
	}
}

func TestResolveImageExtractsSabyPhotoURL(t *testing.T) {
	t.Parallel()
	parameters, _ := json.Marshal(map[string]string{
		"PhotoURL": "http://cdn.example.test/image.jpg",
	})
	value := "https://online.sbis.ru/img?params=" +
		base64.RawURLEncoding.EncodeToString(parameters)

	if got := resolveImage(value); got != "https://cdn.example.test/image.jpg" {
		t.Fatalf("resolveImage() = %q", got)
	}
}

func signedToken(
	t *testing.T,
	privateKey *rsa.PrivateKey,
	keyID string,
	claims map[string]any,
) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": keyID})
	payload, _ := json.Marshal(claims)
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := strings.Join([]string{encodedHeader, encodedPayload}, ".")
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestOIDCVerifierPinsImmutableRepositoryIdentity(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jsonWebKeySet{Keys: []jsonWebKey{{KeyID: "test-key", Algorithm: "RS256", KeyType: "RSA", Modulus: base64.RawURLEncoding.EncodeToString(key.N.Bytes()), Exponent: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	}))
	defer server.Close()
	verifier := NewOIDCVerifier()
	verifier.jwksURL = server.URL
	verifier.now = func() time.Time { return now }
	for _, tt := range []struct{ name, repositoryID, ownerID string }{
		{"recreated name", "999", expectedOwnerID}, {"transferred owner", expectedRepositoryID, "999"}, {"missing IDs", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			token := signedToken(t, key, "test-key", map[string]any{"iss": expectedIssuer, "aud": expectedAudience, "exp": now.Unix() + 300, "nbf": now.Unix() - 10, "repository": expectedRepository, "repository_id": tt.repositoryID, "repository_owner_id": tt.ownerID, "ref": expectedRef, "workflow_ref": expectedWorkflow})
			if err := verifier.Verify(context.Background(), token); err == nil {
				t.Fatal("accepted different immutable identity with matching repository name")
			}
		})
	}
}
