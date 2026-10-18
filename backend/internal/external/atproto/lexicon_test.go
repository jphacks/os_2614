package atproto

import (
	"encoding/json"
	"strings"
	"testing"
)

const testCID = "bafyreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxgf5kpqcsgz7soitae"

func TestValidateRecordFeedPost(t *testing.T) {
	ok := map[string]string{
		"minimal":      `{"text":"こんにちは","createdAt":"2026-10-17T03:12:45.123Z"}`,
		"with $type":   `{"$type":"jp.i32.chain.feed.post","text":"x","createdAt":"2026-10-17T03:12:45.000Z"}`,
		"langs+bsky":   `{"text":"x","langs":["ja","en"],"bsky":{"uri":"at://did:plc:abc/app.bsky.feed.post/3k","cid":"` + testCID + `"},"createdAt":"2026-10-17T03:12:45.000Z"}`,
		"self labels":  `{"text":"x","labels":{"$type":"com.atproto.label.defs#selfLabels","values":[{"val":"spoiler"},{"val":"sexual"}]},"createdAt":"2026-10-17T03:12:45.000Z"}`,
		"1000 graphes": `{"text":"` + strings.Repeat("あ", 1000) + `","createdAt":"2026-10-17T03:12:45.000Z"}`,
	}
	for name, raw := range ok {
		if err := ValidateRawRecord(CollectionFeedPost, []byte(raw)); err != nil {
			t.Errorf("%s: want ok, got %v", name, err)
		}
	}

	bad := map[string]string{
		"text missing":      `{"createdAt":"2026-10-17T03:12:45.000Z"}`,
		"text too long":     `{"text":"` + strings.Repeat("あ", 1001) + `","createdAt":"2026-10-17T03:12:45.000Z"}`,
		"createdAt missing": `{"text":"x"}`,
		"createdAt bad":     `{"text":"x","createdAt":"yesterday"}`,
		"langs over 3":      `{"text":"x","langs":["ja","en","fr","de"],"createdAt":"2026-10-17T03:12:45.000Z"}`,
		"lang bad":          `{"text":"x","langs":["not a lang!"],"createdAt":"2026-10-17T03:12:45.000Z"}`,
		"bsky no cid":       `{"text":"x","bsky":{"uri":"at://did:plc:abc/app.bsky.feed.post/3k"},"createdAt":"2026-10-17T03:12:45.000Z"}`,
		"labels malformed":  `{"text":"x","labels":{"$type":"com.atproto.label.defs#selfLabels","values":"spoiler"},"createdAt":"2026-10-17T03:12:45.000Z"}`,
	}
	for name, raw := range bad {
		if err := ValidateRawRecord(CollectionFeedPost, []byte(raw)); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

func TestValidateRecordProfile(t *testing.T) {
	good := map[string]any{"$type": CollectionChainProfile, "status": "散歩中", "createdAt": "2026-10-17T03:12:45.000Z"}
	if err := ValidateRecord(CollectionChainProfile, good); err != nil {
		t.Fatalf("want ok: %v", err)
	}
	if err := ValidateRecord(CollectionChainProfile, map[string]any{"status": ""}); err == nil {
		t.Error("createdAt missing: want error")
	}
	long := map[string]any{"status": strings.Repeat("あ", 41), "createdAt": "2026-10-17T03:12:45.000Z"}
	if err := ValidateRecord(CollectionChainProfile, long); err == nil {
		t.Error("status 41 graphemes: want error")
	}
}

func TestParseFeedPostSelfLabels(t *testing.T) {
	raw := `{"text":"x","labels":{"$type":"com.atproto.label.defs#selfLabels","values":[{"val":"spoiler"},{"val":"spoiler"},{"val":"porn"}]},"createdAt":"2026-10-17T03:12:45.000Z"}`
	rec, err := ParseFeedPost(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(rec.SelfLabels, ","); got != "spoiler,porn" {
		t.Fatalf("SelfLabels = %q", got)
	}
	if SelfLabelsRecord(nil) != nil {
		t.Error("SelfLabelsRecord(nil) should be nil")
	}
}
