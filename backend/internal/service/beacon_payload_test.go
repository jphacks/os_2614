package service

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type beaconPayloadCase struct {
	Token   string `json:"token"`
	Payload string `json:"payload"`
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex.DecodeString(%q): %v", s, err)
	}
	return b
}

func TestExtractBeaconTokenContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "beacon", "payload.json"))
	if err != nil {
		t.Fatalf("read payload.json: %v", err)
	}
	var doc struct {
		Cases []beaconPayloadCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode payload.json: %v", err)
	}
	if len(doc.Cases) != 3 {
		t.Fatalf("cases = %d, want 3", len(doc.Cases))
	}

	for i, c := range doc.Cases {
		payload := mustHex(t, c.Payload)
		if len(payload) != 26 {
			t.Fatalf("case %d: payload is %d bytes, want 26", i, len(payload))
		}
		got, ok := ExtractBeaconToken(payload)
		if !ok {
			t.Fatalf("case %d: ExtractBeaconToken ok = false", i)
		}
		want := mustHex(t, c.Token)
		if !bytes.Equal(got, want) {
			t.Fatalf("case %d: token = %x, want %x", i, got, want)
		}
	}
}

func TestExtractBeaconTokenRejects(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"bad flags":   "030106",
		"bad magic":   "02010616ffffff414801000102030405060708090a0b0c0d0e0f",
		"bad company": "02010616fffeff434801000102030405060708090a0b0c0d0e0f",
		"bad version": "02010616ffffff434802000102030405060708090a0b0c0d0e0f",
		"truncated":   "02010616ffffff43480100010203",
	}
	for name, payloadHex := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := ExtractBeaconToken(mustHex(t, payloadHex)); ok {
				t.Fatalf("ok = true, want false")
			}
		})
	}
}
