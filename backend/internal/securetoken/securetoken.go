// Package securetoken creates unguessable tokens for links and sessions, and the hashes we
// store instead of the tokens themselves.
package securetoken

import (
	"crypto/rand"
	"crypto/sha256"
)

// New returns 128 bits of randomness as 26 base32 characters (crypto/rand.Text, Go 1.24+).
func New() string {
	return rand.Text()
}

// Hash is SHA-256, not a slow password hash: the token is already long and random,
// so a fast hash is enough. Only the hash is stored, so a leaked database cannot be
// used to log in or open someone's order.
func Hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
