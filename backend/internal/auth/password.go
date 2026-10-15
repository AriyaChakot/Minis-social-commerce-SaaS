// Package auth handles identity: passwords, sessions, and who may access which tenant.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters: the OWASP Password Storage Cheat Sheet minimum (19 MiB, 2 passes, 1 thread).
// They are stored inside every hash, so they can be raised later without breaking old hashes.
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
)

var errMalformedHash = errors.New("auth: malformed password hash")

var b64 = base64.RawStdEncoding

// HashPassword returns an encoded Argon2id hash in the common "PHC string" format:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
//
// A new random salt each time means two users with the same password get different hashes.
func HashPassword(password string) string {
	salt := make([]byte, argonSaltLen)
	rand.Read(salt) // never fails since Go 1.24 (it crashes the program instead of returning an error)

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// VerifyPassword reports whether password matches the encoded hash.
// It re-derives the key with the salt and parameters stored in the hash and compares in constant time.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$") // "", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errMalformedHash
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, errMalformedHash
	}

	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, errMalformedHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, errMalformedHash
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	// ConstantTimeCompare takes the same time whether the first or last byte differs,
	// so an attacker cannot learn anything from how long the comparison took.
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
