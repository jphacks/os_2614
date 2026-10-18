// Package lexicons は CHAIN の lexicon JSON を埋め込む（specification/backend/19-openness.md §6.1）。
package lexicons

import "embed"

// FS は jp.i32.chain.* の lexicon と、それが参照する com.atproto.* の写し（deps/）。
//
//go:embed *.json deps/*.json
var FS embed.FS
