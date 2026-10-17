package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Signer は署名付きのメディア URL を作る（10 §4.4）。
// sig = base64url(HMAC-SHA256(CHAIN_JWT_KEY, id|v|exp))。
type Signer struct {
	Key []byte
}

func NewSigner(key []byte) Signer { return Signer{Key: key} }

func (s Signer) mac(id string, v Variant, exp int64) []byte {
	m := hmac.New(sha256.New, s.Key)
	m.Write([]byte(id + "|" + string(v) + "|" + strconv.FormatInt(exp, 10)))
	return m.Sum(nil)
}

func (s Signer) Sign(id string, v Variant, exp int64) string {
	return base64.RawURLEncoding.EncodeToString(s.mac(id, v, exp))
}

// Verify は署名と有効期限を確かめる。v は "full" / "thumb"、now は現在時刻。
func (s Signer) Verify(id string, v Variant, exp int64, sig string, now time.Time) bool {
	if exp <= now.Unix() {
		return false
	}
	want := s.mac(id, v, exp)
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return false
	}
	return hmac.Equal(got, want)
}

// URL は /media/{id}?v=&exp=&sig= の絶対 URL を組み立てる。publicURL は末尾スラッシュなし。
func (s Signer) URL(publicURL, id string, v Variant, exp int64) string {
	q := url.Values{}
	q.Set("v", string(v))
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", s.Sign(id, v, exp))
	return strings.TrimRight(publicURL, "/") + "/media/" + id + "?" + q.Encode()
}
