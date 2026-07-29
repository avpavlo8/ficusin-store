														package auth

import (
  	"crypto/rand"
  	"crypto/sha256"
  	"encoding/binary"
  	"encoding/hex"
  	"fmt"
  )

// GenerateOTPCode returns a random 4-digit numeric one-time code, e.g. "0427".
func GenerateOTPCode() (string, error) {
  	buffer := make([]byte, 8)
  	if _, err := rand.Read(buffer); err != nil {
      		return "", err
      	}
  	value := binary.BigEndian.Uint64(buffer) % 10000
  	return fmt.Sprintf("%04d", value), nil
  }

// hashOTPCode hashes a code so raw codes are never stored at rest.
func hashOTPCode(phone, code string) string {
  	sum := sha256.Sum256([]byte("otp:" + phone + ":" + code))
  	return hex.EncodeToString(sum[:])
  }
