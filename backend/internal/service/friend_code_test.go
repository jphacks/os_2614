package service

import (
	"strings"
	"testing"
)

func TestInviteCodeFormat(t *testing.T) {
	ids := &countingBytes{}
	for i := 0; i < 100; i++ {
		code := newInviteCode(ids)
		if len(code) != inviteCodeLength {
			t.Fatalf("code %q has %d chars, want %d", code, len(code), inviteCodeLength)
		}
		for _, r := range code {
			if !strings.ContainsRune(crockfordAlphabet, r) {
				t.Fatalf("code %q contains %q, which is not Crockford Base32", code, r)
			}
		}
		if got := displayInviteCode(code); got != code[:4]+"-"+code[4:] {
			t.Fatalf("displayInviteCode(%q) = %q", code, got)
		}
	}
}

func TestInviteCodeIsDeterministic(t *testing.T) {
	a := newInviteCode(&fixedBytes{raw: []byte{0x00, 0x00, 0x00, 0x00, 0x00}})
	b := newInviteCode(&fixedBytes{raw: []byte{0xff, 0xff, 0xff, 0xff, 0xff}})
	if a != "00000000" {
		t.Fatalf("zero code = %q, want 00000000", a)
	}
	if b != "ZZZZZZZZ" {
		t.Fatalf("max code = %q, want ZZZZZZZZ", b)
	}
}

func TestNormalizeInviteCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"display form", "7KQ2-M9XD", "7KQ2M9XD", true},
		{"lowercase without hyphen", "7kq2m9xd", "7KQ2M9XD", true},
		{"mixed case with spaces", " 7kq2-m9xd ", "7KQ2M9XD", true},
		{"crockford I and L fold to 1", "IL2Q-M9XD", "112QM9XD", true},
		{"crockford O folds to 0", "OKQ2-M9XD", "0KQ2M9XD", true},
		{"too short", "7KQ2M9X", "", false},
		{"too long", "7KQ2M9XDX", "", false},
		{"unknown letter U", "UKQ2-M9XD", "", false},
		{"symbol", "7KQ2_M9XD", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeInviteCode(tc.in)
			if ok != tc.ok {
				t.Fatalf("normalizeInviteCode(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("normalizeInviteCode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHashInviteCode(t *testing.T) {
	// SHA-256("7KQ2M9XD") の hex。DB にはこの形だけを保存する（07 §2.1）。
	const want = "28260557126c16ecfa20c77a89f2da7961bbe4132ee6fc5f5c506b8595f3d165"
	got := hashInviteCode("7KQ2M9XD")
	if got != want {
		t.Fatalf("hashInviteCode = %q, want %q", got, want)
	}
	if hashInviteCode("7KQ2M9XD") != got {
		t.Fatal("hashInviteCode is not deterministic")
	}
	if hashInviteCode("7KQ2M9XE") == got {
		t.Fatal("hashInviteCode collided for different codes")
	}
}

type fixedBytes struct{ raw []byte }

func (f *fixedBytes) NewID() string { return "id" }

func (f *fixedBytes) Bytes(int) []byte {
	out := make([]byte, len(f.raw))
	copy(out, f.raw)
	return out
}

type countingBytes struct{ n byte }

func (c *countingBytes) NewID() string { return "id" }

func (c *countingBytes) Bytes(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = c.n + byte(i)
	}
	c.n++
	return out
}
