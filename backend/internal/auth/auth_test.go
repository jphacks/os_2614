package auth

import (
	"bytes"
	"testing"
	"time"
)

var testKey = bytes.Repeat([]byte{7}, 32)

func TestAccessTokenRoundTrip(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	tok, err := IssueAccess(testKey, "did:plc:alice", "sid-1", now)
	if err != nil {
		t.Fatal(err)
	}

	c, err := ParseAccess(testKey, tok, now.Add(AccessTTL-time.Second))
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if c.DID != "did:plc:alice" || c.SessionID != "sid-1" {
		t.Fatalf("claims = %+v", c)
	}

	if _, err := ParseAccess(testKey, tok, now.Add(AccessTTL+time.Second)); err == nil {
		t.Fatal("expired token accepted")
	}
	if _, err := ParseAccess(bytes.Repeat([]byte{8}, 32), tok, now); err == nil {
		t.Fatal("token with wrong key accepted")
	}
}

func TestEncryptRoundTrip(t *testing.T) {
	sealed, err := Encrypt(testKey, []byte("refresh-token"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Decrypt(testKey, sealed)
	if err != nil || string(plain) != "refresh-token" {
		t.Fatalf("plain = %q, err = %v", plain, err)
	}

	sealed[len(sealed)-1] ^= 1
	if _, err := Decrypt(testKey, sealed); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}
