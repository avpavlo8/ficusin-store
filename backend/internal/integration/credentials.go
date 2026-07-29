package integration

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CredentialStore struct {
	pool       *pgxpool.Pool
	privateKey *rsa.PrivateKey
}

type encryptedEnvelope struct {
	EncryptedKey string `json:"encryptedKey"`
	IV           string `json:"iv"`
	Ciphertext   string `json:"ciphertext"`
	Tag          string `json:"tag"`
}

func NewCredentialStore(pool *pgxpool.Pool, privateKeyPEM string) (*CredentialStore, error) {
	privateKeyPEM = strings.ReplaceAll(strings.TrimSpace(privateKeyPEM), `\n`, "\n")
	if privateKeyPEM == "" {
		return &CredentialStore{pool: pool}, nil
	}
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, errors.New("integration private key is not PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse integration private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("integration private key is not RSA")
	}
	return &CredentialStore{pool: pool, privateKey: rsaKey}, nil
}

func GetCredentials[T any](
	ctx context.Context,
	store *CredentialStore,
	provider string,
) (T, error) {
	var empty T
	if store.privateKey == nil {
		return empty, errors.New("ключ защищённых интеграций не настроен")
	}

	var payload string
	if err := store.pool.QueryRow(ctx, `
		SELECT encrypted_payload
		FROM integration_credentials
		WHERE provider = $1
		LIMIT 1
	`, provider).Scan(&payload); err != nil {
		return empty, fmt.Errorf("load %s credentials: %w", provider, err)
	}

	var envelope encryptedEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return empty, fmt.Errorf("decode %s envelope: %w", provider, err)
	}
	decode := func(value string) ([]byte, error) {
		return base64.StdEncoding.DecodeString(value)
	}
	encryptedKey, err := decode(envelope.EncryptedKey)
	if err != nil {
		return empty, fmt.Errorf("decode encrypted key: %w", err)
	}
	aesKey, err := rsa.DecryptOAEP(sha256.New(), nil, store.privateKey, encryptedKey, nil)
	if err != nil {
		return empty, fmt.Errorf("decrypt integration key: %w", err)
	}
	iv, err := decode(envelope.IV)
	if err != nil {
		return empty, fmt.Errorf("decode integration iv: %w", err)
	}
	encrypted, err := decode(envelope.Ciphertext)
	if err != nil {
		return empty, fmt.Errorf("decode integration ciphertext: %w", err)
	}
	tag, err := decode(envelope.Tag)
	if err != nil {
		return empty, fmt.Errorf("decode integration tag: %w", err)
	}

	blockCipher, err := aes.NewCipher(aesKey)
	if err != nil {
		return empty, fmt.Errorf("create integration cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return empty, fmt.Errorf("create integration gcm: %w", err)
	}
	clear, err := gcm.Open(nil, iv, append(encrypted, tag...), nil)
	if err != nil {
		return empty, fmt.Errorf("decrypt %s credentials: %w", provider, err)
	}

	var credentials T
	if err := json.Unmarshal(clear, &credentials); err != nil {
		return empty, fmt.Errorf("decode %s credentials: %w", provider, err)
	}
	return credentials, nil
}
