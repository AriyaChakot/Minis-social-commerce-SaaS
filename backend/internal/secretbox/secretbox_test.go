package secretbox

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

func newBox(t *testing.T) *Box {
	t.Helper()
	key := make([]byte, KeySize)
	rand.Read(key)
	b, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealOpen(t *testing.T) {
	b := newBox(t)
	secret := []byte("0123456789abcdef0123456789abcdef")

	sealed := b.Seal(secret, "line_channels/shop-a/channel_secret")
	if bytes.Contains(sealed, secret) {
		t.Fatal("the sealed value must not contain the plaintext")
	}
	got, err := b.Open(sealed, "line_channels/shop-a/channel_secret")
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("open: got %q, %v", got, err)
	}

	// Same plaintext twice: different ciphertexts (random nonce), so equal secrets are not visible.
	if bytes.Equal(sealed, b.Seal(secret, "line_channels/shop-a/channel_secret")) {
		t.Fatal("sealing twice must give different values")
	}
}

func TestOpenRefuses(t *testing.T) {
	b := newBox(t)
	sealed := b.Seal([]byte("token"), "ctx-a")

	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1

	cases := map[string]struct {
		box     *Box
		sealed  []byte
		context string
	}{
		"other context (value moved to another row)": {b, sealed, "ctx-b"},
		"changed byte":    {b, tampered, "ctx-a"},
		"other key":       {newBox(t), sealed, "ctx-a"},
		"too short":       {b, sealed[:5], "ctx-a"},
		"unknown version": {b, append([]byte{9}, sealed[1:]...), "ctx-a"},
		"empty":           {b, nil, "ctx-a"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tc.box.Open(tc.sealed, tc.context); !errors.Is(err, ErrDecrypt) {
				t.Fatalf("want ErrDecrypt, got %v", err)
			}
		})
	}
}

func TestParseKey(t *testing.T) {
	good := base64.StdEncoding.EncodeToString(make([]byte, KeySize))
	if _, err := ParseKey(good); err != nil {
		t.Fatalf("valid key: %v", err)
	}
	for _, bad := range []string{"", "not base64!", base64.StdEncoding.EncodeToString(make([]byte, 16))} {
		if _, err := ParseKey(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}
