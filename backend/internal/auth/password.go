package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/crypto/scrypt"
)

const (
	scryptN         = 16384
	scryptR         = 8
	scryptP         = 1
	passwordKeySize = 64
)

func PasswordIsAcceptable(password string) bool {
	length := len([]rune(password))
	if length < 10 || length > 128 {
		return false
	}

	var hasLetter, hasDigit bool
	for _, character := range password {
		hasLetter = hasLetter || unicode.IsLetter(character)
		hasDigit = hasDigit || character >= '0' && character <= '9'
	}
	return hasLetter && hasDigit
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	derived, err := scrypt.Key(
		[]byte(password),
		salt,
		scryptN,
		scryptR,
		scryptP,
		passwordKeySize,
	)
	if err != nil {
		return "", fmt.Errorf("derive password hash: %w", err)
	}

	return fmt.Sprintf(
		"scrypt$%d$%d$%d$%s$%s",
		scryptN,
		scryptR,
		scryptP,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(derived),
	), nil
}

func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "scrypt" {
		return false
	}

	n, errN := strconv.Atoi(parts[1])
	r, errR := strconv.Atoi(parts[2])
	p, errP := strconv.Atoi(parts[3])
	if err := errors.Join(errN, errR, errP); err != nil {
		return false
	}
	if n != scryptN || r != scryptR || p != scryptP {
		return false
	}

	salt, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return false
	}

	actual, err := scrypt.Key([]byte(password), salt, n, r, p, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(expected, actual) == 1
}
