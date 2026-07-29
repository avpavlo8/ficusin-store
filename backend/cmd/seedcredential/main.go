// Command seedcredential encrypts a JSON credentials payload for a given
// integration provider (e.g. "telegram", "cdek") using the same RSA-OAEP +
// AES-GCM envelope scheme as internal/integration/credentials.go, and
// prints a ready-to-run SQL statement that upserts it into the
// integration_credentials table.
//
// The plaintext payload is read from stdin so it never lands in shell
// history or process listings. Usage:
//
//	echo '{"botToken":"123456:ABC..."}' | \
//		go run ./backend/cmd/seedcredential -provider telegram -key private.pem
package main

import (
  	"crypto/aes"
  	"crypto/cipher"
  	"crypto/rand"
  	"crypto/rsa"
  	"crypto/sha256"
  	"crypto/x509"
  	"encoding/base64"
  	"encoding/json"
  	"encoding/pem"
  	"errors"
  	"flag"
  	"fmt"
  	"io"
  	"os"
  	"strings"
  )

type envelope struct {
  	EncryptedKey string `json:"encryptedKey"`
  	IV           string `json:"iv"`
  	Ciphertext   string `json:"ciphertext"`
  	Tag          string `json:"tag"`
  }

func main() {
  	provider := flag.String("provider", "", "integration provider name, e.g. telegram or cdek")
  	keyPath := flag.String("key", "", "path to the INTEGRATION_SECRETS_PRIVATE_KEY PEM file")
  	flag.Parse()

  	if *provider == "" || *keyPath == "" {
      		fmt.Fprintln(os.Stderr, "usage: seedcredential -provider <name> -key <private-key.pem> < payload.json")
      		os.Exit(1)
      	}

  	payload, err := io.ReadAll(os.Stdin)
  	if err != nil {
      		fail("read payload from stdin", err)
      	}
  	var check any
  	if err := json.Unmarshal(payload, &check); err != nil {
      		fail("payload is not valid JSON", err)
      	}

  	privateKey, err := loadPrivateKey(*keyPath)
  	if err != nil {
      		fail("load private key", err)
      	}

  	sealed, err := seal(&privateKey.PublicKey, payload)
  	if err != nil {
      		fail("encrypt payload", err)
      	}
  	sealedJSON, err := json.Marshal(sealed)
  	if err != nil {
      		fail("encode envelope", err)
      	}

  	escaped := strings.ReplaceAll(string(sealedJSON), "'", "''")
  	fmt.Printf(`INSERT INTO integration_credentials (provider, encrypted_payload)
    VALUES ('%s', '%s')
    ON CONFLICT (provider) DO UPDATE
    SET encrypted_payload = EXCLUDED.encrypted_payload, updated_at = CURRENT_TIMESTAMP;
    `, *provider, escaped)
  }

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
  	raw, err := os.ReadFile(path)
  	if err != nil {
      		return nil, err
      	}
  	block, _ := pem.Decode(raw)
  	if block == nil {
      		return nil, errors.New("not a PEM file")
      	}
  	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
  	if err != nil {
      		return nil, err
      	}
  	rsaKey, ok := key.(*rsa.PrivateKey)
  	if !ok {
      		return nil, errors.New("private key is not RSA")
      	}
  	return rsaKey, nil
  }

func seal(publicKey *rsa.PublicKey, payload []byte) (envelope, error) {
  	aesKey := make([]byte, 32)
  	if _, err := rand.Read(aesKey); err != nil {
      		return envelope{}, err
      	}
  	block, err := aes.NewCipher(aesKey)
  	if err != nil {
      		return envelope{}, err
      	}
  	gcm, err := cipher.NewGCM(block)
  	if err != nil {
      		return envelope{}, err
      	}
  	iv := make([]byte, gcm.NonceSize())
  	if _, err := rand.Read(iv); err != nil {
      		return envelope{}, err
      	}
  	sealed := gcm.Seal(nil, iv, payload, nil)
  	tagSize := gcm.Overhead()
  	ciphertext := sealed[:len(sealed)-tagSize]
  	tag := sealed[len(sealed)-tagSize:]

  	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, aesKey, nil)
  	if err != nil {
      		return envelope{}, err
      	}

  	return envelope{
      		EncryptedKey: base64.StdEncoding.EncodeToString(encryptedKey),
      		IV:           base64.StdEncoding.EncodeToString(iv),
      		Ciphertext:   base64.StdEncoding.EncodeToString(ciphertext),
      		Tag:          base64.StdEncoding.EncodeToString(tag),
      	}, nil
  }

func fail(context string, err error) {
  	fmt.Fprintf(os.Stderr, "%s: %v\n", context, err)
  	os.Exit(1)
  }
