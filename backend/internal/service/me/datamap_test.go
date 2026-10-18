package me_test

import (
	"testing"

	mesvc "github.com/applemango/chain/internal/service/me"
)

func TestPDSHost(t *testing.T) {
	cases := map[string]string{
		"https://bsky.social":           "bsky.social",
		"https://pds.example:8443/xrpc": "pds.example",
		"":                              "",
		"not a url":                     "",
	}
	for in, want := range cases {
		if got := mesvc.PDSHost(in); got != want {
			t.Errorf("PDSHost(%q) = %q, want %q", in, got, want)
		}
	}
}
