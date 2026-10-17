// Package kv は Redis の接続とキー名を持つ。キーの一覧は specification/backend/04-data-model.md §3。
package kv

import (
	"context"
	"strconv"

	"github.com/redis/go-redis/v9"
)

func Open(ctx context.Context, url string) (*redis.Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(opt)
	if err := c.Ping(ctx).Err(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// キー名は文字列を直接書かず、この関数群を通す。

func SessionKey(sid string) string { return "sess:" + sid }

func LockKey(name string) string { return "lock:" + name }

func OAuthRequestKey(state string) string { return "oauth:req:" + state }

func OneTimeCodeKey(code string) string { return "otc:" + code }

func RateLimitKey(bucket, did string, window int64) string {
	return "rl:" + bucket + ":" + did + ":" + strconv.FormatInt(window, 10)
}

// RateLimitAuthKey は /auth/* の IP 単位のレート制限カウンタ（02 §5、04 §3）。
func RateLimitAuthKey(ip string, window int64) string {
	return "rl:auth:" + ip + ":" + strconv.FormatInt(window, 10)
}

// 05 §2.1: beacon token の逆引き。hashHex は SHA-256(token) の hex。
func BeaconTokenKey(hashHex string) string { return "bt:" + hashHex }

// 05 §4: 最新位置の hash と、位置共有・グループ判定用の geo インデックス。
func LocationKey(did string) string { return "loc:" + did }
func GeoUsersKey() string           { return "geo:users" }

// 09 §5: active なグループの中心（GEOSEARCH の対象）。
func GeoGroupsKey() string { return "geo:groups" }

// 05 §3: 観測バケットと直近に観測した相手。
func SightingKey(observer, observed string, bucket int64) string {
	return "sgt:" + observer + ":" + observed + ":" + strconv.FormatInt(bucket, 10)
}
func BleRecentKey(did string) string { return "ble:recent:" + did }

// 08 §3: フレンド近接の連続性。S3 では読み出しだけ使う。
func PeerEdgeKey(a, b string) string { return "pe:" + a + ":" + b }
func PeerActiveKey() string          { return "pe:active" }

// AccessKey はアクセス判定の結果（04 §3 acc:{space}:{did}:{mode}、10 §5）のキー。
func AccessKey(spaceID, did, mode string) string {
	return "acc:" + spaceID + ":" + did + ":" + mode
}

// AccessPrefix は Space の判定結果をまとめて消すための接頭辞。
func AccessPrefix(spaceID string) string { return "acc:" + spaceID + ":" }

// ModerationQueueKey は moderation の再判定待ち（11 §3）。
func ModerationQueueKey() string { return "modq" }

// 09 §8.4: 自動グループの候補と、続けて見つかった回数。
func NaturalCandidateKey(geohash7, slot string) string {
	return "ng:cand:" + geohash7 + ":" + slot
}

func NaturalCandidatePrefix() string { return "ng:cand:" }

// 13 §3: 個人の統計のキャッシュ（TTL 5 分）。
func StatsKey(did string) string { return "stats:" + did }
