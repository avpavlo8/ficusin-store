// Package webpush delivers a notification to a browser's push service.
//
// It implements the two specs by hand rather than pulling in a dependency:
// RFC 8291 (encrypting the payload for the browser) and RFC 8292 (VAPID,
// which proves to the push service that we are who we claim). Both are small
// enough that the standard library covers everything needed — ECDH, HKDF,
// AES-GCM and ECDSA are all in crypto/*.
package webpush

import (
	"bytes"
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
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrGone means the browser threw the subscription away — the row should be
// marked expired rather than retried.
var ErrGone = errors.New("push subscription no longer exists")

// recordSize is the largest payload a push service must accept, and what the
// aes128gcm header advertises.
const recordSize = 4096

type Subscription struct {
	Endpoint string
	// P256dh and Auth are the base64url values the browser handed to the page.
	P256dh string
	Auth   string
}

type Client struct {
	privateKey *ecdsa.PrivateKey
	publicKey  []byte
	subject    string
	httpClient *http.Client
}

// NewClient takes the VAPID key pair as the base64url strings kept in the
// environment. The public key is also what the browser needs when it
// subscribes, so it is exposed again via PublicKey.
func NewClient(privateKey, publicKey, subject string) (*Client, error) {
	rawPrivate, err := decode(privateKey)
	if err != nil {
		return nil, fmt.Errorf("decode VAPID private key: %w", err)
	}
	rawPublic, err := decode(publicKey)
	if err != nil {
		return nil, fmt.Errorf("decode VAPID public key: %w", err)
	}
	if len(rawPrivate) != 32 {
		return nil, errors.New("VAPID private key must be 32 bytes")
	}
	if len(rawPublic) != 65 || rawPublic[0] != 4 {
		return nil, errors.New("VAPID public key must be an uncompressed P-256 point")
	}
	if strings.TrimSpace(subject) == "" {
		return nil, errors.New("VAPID subject is required")
	}

	key := new(ecdsa.PrivateKey)
	key.Curve = elliptic.P256()
	key.D = new(big.Int).SetBytes(rawPrivate)
	key.X, key.Y = elliptic.Unmarshal(elliptic.P256(), rawPublic)
	if key.X == nil {
		return nil, errors.New("VAPID public key is not on the curve")
	}

	return &Client{
		privateKey: key,
		publicKey:  rawPublic,
		subject:    strings.TrimSpace(subject),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (client *Client) PublicKey() string {
	return base64.RawURLEncoding.EncodeToString(client.publicKey)
}

// Send encrypts payload for the subscription and hands it to the push
// service. A nil error means the service accepted it for delivery — not that
// anyone read it.
func (client *Client) Send(subscription Subscription, payload []byte, ttl int) error {
	body, err := encrypt(subscription, payload)
	if err != nil {
		return err
	}

	request, err := http.NewRequest(http.MethodPost, subscription.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build push request: %w", err)
	}
	authorization, err := client.authorization(subscription.Endpoint)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Encoding", "aes128gcm")
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("TTL", fmt.Sprint(ttl))
	request.Header.Set("Urgency", "normal")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send push: %w", err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
		return ErrGone
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return nil
	default:
		return fmt.Errorf("push service answered %d", response.StatusCode)
	}
}

// authorization builds the VAPID header: a JWT signed with our key, plus the
// public half so the service can check it.
func (client *Client) authorization(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	header := encodeJSON(map[string]string{"typ": "JWT", "alg": "ES256"})
	claims := encodeJSON(map[string]any{
		"aud": parsed.Scheme + "://" + parsed.Host,
		// Twelve hours is well inside the 24 the spec allows.
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": client.subject,
	})
	signingInput := header + "." + claims

	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, client.privateKey, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign VAPID token: %w", err)
	}
	// JWS wants the raw pair, each padded to the curve size — not ASN.1.
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])

	token := signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
	return "vapid t=" + token + ", k=" + client.PublicKey(), nil
}

// encrypt produces an aes128gcm record as RFC 8291 describes: a fresh key
// pair per message, a shared secret with the browser, and one AES-GCM record
// carrying the payload.
func encrypt(subscription Subscription, payload []byte) ([]byte, error) {
	clientPublic, err := decode(subscription.P256dh)
	if err != nil {
		return nil, fmt.Errorf("decode subscription key: %w", err)
	}
	authSecret, err := decode(subscription.Auth)
	if err != nil {
		return nil, fmt.Errorf("decode subscription auth: %w", err)
	}

	curve := ecdh.P256()
	browserKey, err := curve.NewPublicKey(clientPublic)
	if err != nil {
		return nil, fmt.Errorf("subscription key is not a P-256 point: %w", err)
	}
	ourKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}
	shared, err := ourKey.ECDH(browserKey)
	if err != nil {
		return nil, fmt.Errorf("derive shared secret: %w", err)
	}
	ourPublic := ourKey.PublicKey().Bytes()

	// The browser's key comes first in the info string; getting this order
	// wrong yields a message that decrypts to nothing.
	keyInfo := append([]byte("WebPush: info\x00"), clientPublic...)
	keyInfo = append(keyInfo, ourPublic...)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, fmt.Errorf("derive input keying material: %w", err)
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	contentKey, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, fmt.Errorf("derive content key: %w", err)
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, fmt.Errorf("derive nonce: %w", err)
	}

	block, err := aes.NewCipher(contentKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// 0x02 marks the last record; everything fits in one for our messages.
	plaintext := append(append([]byte{}, payload...), 0x02)
	if len(plaintext)+aead.Overhead()+86 > recordSize {
		return nil, errors.New("push payload is too large")
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	body := make([]byte, 0, 21+len(ourPublic)+len(ciphertext))
	body = append(body, salt...)
	body = binary.BigEndian.AppendUint32(body, recordSize)
	body = append(body, byte(len(ourPublic)))
	body = append(body, ourPublic...)
	body = append(body, ciphertext...)
	return body, nil
}

func encodeJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// decode accepts both padded and unpadded base64url, because browsers and
// key generators disagree about padding.
func decode(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
}
