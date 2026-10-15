package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash := HashPassword("correct horse battery staple")

	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}

	ok, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil || !ok {
		t.Fatalf("right password: want ok, got ok=%v err=%v", ok, err)
	}

	ok, err = VerifyPassword("wrong password", hash)
	if err != nil || ok {
		t.Fatalf("wrong password: want !ok, got ok=%v err=%v", ok, err)
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	if HashPassword("same") == HashPassword("same") {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	for _, bad := range []string{
		"",
		"plain-text-password",
		"$2a$10$bcrypthashlooksdifferent",
		"$argon2id$v=19$m=19456,t=2,p=1$not-base64!$abc",
	} {
		if _, err := VerifyPassword("x", bad); err == nil {
			t.Errorf("VerifyPassword(%q): want error, got nil", bad)
		}
	}
}
