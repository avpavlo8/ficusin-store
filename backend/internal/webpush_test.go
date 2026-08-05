package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
)

// The browser side of RFC 8291, written out here so the test can read what
// the client wrote. If the two ever disagree, the encryption is wrong and a
// notification would arrive as an empty bubble on someone's phone.
func decryptAsBrowser(t *testing.T, body []byte, browserKey *ecdh.PrivateKey, authSecret []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("body is %d bytes, too short for a header", len(body))
	}
	salt := body[:16]
	if size := binary.BigEndian.Uint32(body[16:20]); size != recordSize {
		t.Fatalf("record size = %d, want %d", size, recordSize)
	}
	keyLength := int(body[20])
	senderPublic := body[21 : 21+keyLength]
	ciphertext := body[21+keyLength:]

	senderKey, err := ecdh.P256().NewPublicKey(senderPublic)
	if err != nil {
		t.Fatalf("sender key: %v", err)
	}
	shared, err := browserKey.ECDH(senderKey)
	if err != nil {
		t.Fatalf("ECDH: %v", err)
	}

	keyInfo := append([]byte("WebPush: info\x00"), browserKey.PublicKey().Bytes()...)
	keyInfo = append(keyInfo, senderPublic...)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, string(keyInfo), 32)
	if err != nil {
		t.Fatalf("HKDF ikm: %v", err)
	}
	contentKey, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		t.Fatalf("HKDF key: %v", err)
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		t.Fatalf("HKDF nonce: %v", err)
	}

	block, err := aes.NewCipher(contentKey)
	if err != nil {
		t.Fatalf("AES: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("GCM: %v", err)
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("the browser could not decrypt the message: %v", err)
	}
	if len(plaintext) == 0 || plaintext[len(plaintext)-1] != 0x02 {
		t.Fatalf("padding delimiter is missing: %v", plaintext)
	}
	return plaintext[:len(plaintext)-1]
}

func TestEncryptedPayloadCanBeReadByTheBrowser(t *testing.T) {
	t.Parallel()

	browserKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate browser key: %v", err)
	}
	authSecret := make([]byte, 16)
	if _, err := rand.Read(authSecret); err != nil {
		t.Fatalf("generate auth secret: %v", err)
	}

	message := []byte(`{"title":"Фикусин","body":"Заказ ZR-1 собран"}`)
	body, err := encrypt(Subscription{
		P256dh: base64.RawURLEncoding.EncodeToString(browserKey.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(authSecret),
	}, message)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if got := decryptAsBrowser(t, body, browserKey, authSecret); string(got) != string(message) {
		t.Fatalf("decrypted %q, want %q", got, message)
	}
}

// Subscriptions arrive from the browser with padding stripped, but keys
// pasted from other tools often keep it.
func TestEncryptAcceptsPaddedKeys(t *testing.T) {
	t.Parallel()

	browserKey, _ := ecdh.P256().GenerateKey(rand.Reader)
	authSecret := make([]byte, 16)
	_, _ = rand.Read(authSecret)

	if _, err := encrypt(Subscription{
		P256dh: base64.URLEncoding.EncodeToString(browserKey.PublicKey().Bytes()),
		Auth:   base64.URLEncoding.EncodeToString(authSecret),
	}, []byte("привет")); err != nil {
		t.Fatalf("padded keys rejected: %v", err)
	}
}

func testClient(t *testing.T) *Client {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate VAPID key: %v", err)
	}
	private := make([]byte, 32)
	key.D.FillBytes(private)
	public := elliptic.Marshal(elliptic.P256(), key.X, key.Y)

	client, err := NewClient(
		base64.RawURLEncoding.EncodeToString(private),
		base64.RawURLEncoding.EncodeToString(public),
		"mailto:shop@ficusin.ru",
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestAuthorizationIsAVerifiableToken(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	header, err := client.authorization("https://fcm.googleapis.com/fcm/send/abc123")
	if err != nil {
		t.Fatalf("authorization: %v", err)
	}
	if !strings.HasPrefix(header, "vapid t=") || !strings.Contains(header, ", k=") {
		t.Fatalf("header is not a VAPID header: %s", header)
	}

	token := strings.TrimPrefix(strings.Split(header, ", k=")[0], "vapid t=")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}

	var claims struct {
		Audience string `json:"aud"`
		Subject  string `json:"sub"`
		Expires  int64  `json:"exp"`
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("parse claims: %v", err)
	}
	// The audience must be the push service's origin and nothing more —
	// including the path would make the service reject the token.
	if claims.Audience != "https://fcm.googleapis.com" {
		t.Fatalf("aud = %q", claims.Audience)
	}
	if claims.Subject != "mailto:shop@ficusin.ru" {
		t.Fatalf("sub = %q", claims.Subject)
	}
	if claims.Expires == 0 {
		t.Fatal("exp is missing")
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if len(signature) != 64 {
		t.Fatalf("signature is %d bytes, want the raw 64-byte pair", len(signature))
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(&client.privateKey.PublicKey, digest[:], r, s) {
		t.Fatal("signature does not verify against our own public key")
	}
}

func TestNewClientRejectsBrokenKeys(t *testing.T) {
	t.Parallel()

	for name, run := range map[string]func() error{
		"короткий приватный ключ": func() error {
			_, err := NewClient(base64.RawURLEncoding.EncodeToString([]byte("short")), validPublic(), "mailto:a@b.ru")
			return err
		},
		"публичный ключ не точка": func() error {
			_, err := NewClient(validPrivate(), base64.RawURLEncoding.EncodeToString([]byte("nope")), "mailto:a@b.ru")
			return err
		},
		"пустой subject": func() error {
			_, err := NewClient(validPrivate(), validPublic(), "  ")
			return err
		},
	} {
		if err := run(); err == nil {
			t.Fatalf("%s: ошибки нет, а должна быть", name)
		}
	}
}

func validPrivate() string {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	private := make([]byte, 32)
	key.D.FillBytes(private)
	return base64.RawURLEncoding.EncodeToString(private)
}

func validPublic() string {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), key.X, key.Y))
}
