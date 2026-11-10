package application

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

const ownerTokenBytes = 32

// MintOwnerToken returns a base64url token (no padding) and its SHA-256 hash (32 bytes).
func MintOwnerToken() (plaintext string, hash []byte, err error) {
	raw := make([]byte, ownerTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(raw), sum[:], nil
}

// HashOwnerToken hashes a presented bearer token the same way MintOwnerToken does.
func HashOwnerToken(plaintext string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil {
		return nil, err
	}
	if len(raw) != ownerTokenBytes {
		return nil, errInvalidToken
	}
	sum := sha256.Sum256(raw)
	return sum[:], nil
}

// OwnerTokenMatches compares a presented token to a stored hash in constant time.
func OwnerTokenMatches(plaintext string, storedHash []byte) bool {
	got, err := HashOwnerToken(plaintext)
	if err != nil || len(storedHash) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare(got, storedHash) == 1
}

var errInvalidToken = errToken("invalid owner token")

type errToken string

func (e errToken) Error() string { return string(e) }
