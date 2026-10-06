package secretbox

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func testBox(t *testing.T) *Box {
	t.Helper()
	b, err := New(bytes.Repeat([]byte{7}, KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealOpen(t *testing.T) {
	b := testBox(t)
	sealed := b.Seal([]byte("token"), []byte("account-a"))
	if bytes.Contains(sealed, []byte("token")) {
		t.Fatal("ciphertext contains the plaintext")
	}
	plain, err := b.Open(sealed, []byte("account-a"))
	if err != nil || string(plain) != "token" {
		t.Fatalf("Open = %q, %v", plain, err)
	}
	if bytes.Equal(sealed, b.Seal([]byte("token"), []byte("account-a"))) {
		t.Fatal("two identical ciphertexts: nonce reused")
	}
}

func TestOpenRejects(t *testing.T) {
	b := testBox(t)
	sealed := b.Seal([]byte("token"), []byte("account-a"))
	other, _ := New(bytes.Repeat([]byte{8}, KeySize))
	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1

	tests := []struct {
		name    string
		box     *Box
		sealed  []byte
		context string
	}{
		{"other account", b, sealed, "account-b"},
		{"other key", other, sealed, "account-a"},
		{"tampered", b, tampered, "account-a"},
		{"too short", b, sealed[:3], "account-a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.box.Open(tt.sealed, []byte(tt.context)); err == nil {
				t.Fatal("error expected")
			}
		})
	}
}

func TestKeys(t *testing.T) {
	if _, err := New([]byte("short")); err == nil {
		t.Error("short key accepted")
	}
	if _, err := FromBase64("not base64!"); err == nil {
		t.Error("invalid base64 accepted")
	}
	if _, err := FromBase64(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, KeySize))); err != nil {
		t.Errorf("valid key rejected: %v", err)
	}
}
