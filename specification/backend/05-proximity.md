# 05 近接判定（BLE / GPS 共通）

## 1. 共通 interface

draft の「BLE と GPS を switch できる共通 interface」を、判定の種類で 2 つに分ける。

| interface | 問い | 使う機能 | P0 の実装 | 将来の実装 |
| --- | --- | --- | --- | --- |
| `PeerProximity` | ユーザー a と b は時刻 t の近くで近接していたか。u の近くに誰がいるか | すれ違い、Party | BLE | GPS（距離閾値） |
| `ZonePresence` | ユーザー u はゾーン z の中にいるか | グループ | GPS（geofence） | BLE（ゾーン内の在席メンバーを u が観測している） |

```go
package proximity

type Kind string

const (
	KindBLE Kind = "ble"
	KindGPS Kind = "gps"
)

type Neighbor struct {
	DID      syntax.DID
	LastSeen time.Time
	RSSI     *int     // BLE のみ
	DistM    *float64 // GPS のみ
}

type PeerProximity interface {
	Kind() Kind
	// a が b を観測したことを記録する（方向あり）。
	Observe(ctx context.Context, a, b syntax.DID, first, last time.Time, rssi *int) error
	// a→b と b→a の両方が、at の前後 within 以内にあるか。
	Mutual(ctx context.Context, a, b syntax.DID, at time.Time, within time.Duration) (bool, error)
	// u が直近 within 以内に観測した、または u を観測した相手。
	Neighbors(ctx context.Context, u syntax.DID, within time.Duration) ([]Neighbor, error)
}

type Zone struct {
	ID        string
	CenterLat float64
	CenterLon float64
	RadiusM   int
}

type ZonePresence interface {
	Kind() Kind
	// u が maxAge 以内の情報に基づいて z の中にいるか。
	// 判定材料がなければ ErrStale を返す。
	Inside(ctx context.Context, u syntax.DID, z Zone, maxAge time.Duration) (bool, error)
}
```

機能側は具体型ではなくこの interface を受け取る。どの実装を使うかは `main.go` で組み立てる。

## 2. beacon token（P0）

すれ違いと Party のために、端末が BLE で発信する使い捨て ID。ユーザー ID を周囲にばら撒かないためのもの（draft）。

### 2.1 形式

| 項目 | 値 |
| --- | --- |
| 長さ | 16 バイトの暗号学的乱数（`crypto/rand`） |
| 有効窓 | 15 分。`window_start = floor(unix秒 / 900) * 900` |
| 発行単位 | 1 ユーザー × 1 窓につき 1 個 |
| サーバ保存 | `beacon_tokens.token_hash = SHA-256(token)`（逆引き用）と `token_enc`（AES-256-GCM。再取得と、Party 用のフレンド配布（08 §2）に使う） |
| 逆引き | Redis `bt:{hex(token_hash)}` → `{did}|{window_start}`。ミス時は SQLite |

### 2.2 発行 API

`GET /api/v1/beacon/tokens?hours=24`

- 現在の窓から `hours`（1〜24、既定 24）時間分の token を返す。発行済みの窓は同じ token を返す（冪等）。未発行の窓だけ新しく作る
- クライアントは残りが 6 時間を切ったら再取得する。token を使い切ったら発信を止め、同じ token を再利用しない

```json
{
  "windowSec": 900,
  "tokens": [
    { "windowStart": "2026-10-17T03:00:00Z", "token": "base64url(16 bytes)" }
  ]
}
```

### 2.3 BLE advertise の payload（native との契約）

Legacy advertising（31 バイト）に収める。Coded PHY / extended advertising は端末差が未検証（research U2）のため P2 とする。

| AD | バイト | 内容 |
| --- | --- | --- |
| Flags | 3 | `02 01 06` |
| Manufacturer Specific Data | 23 | `16 FF` ＋ company ID `FF FF`（開発用。本番は Bluetooth SIG の割り当てが要る）＋ magic `43 48`（"CH"）＋ version `01` ＋ token 16 バイト |
| 合計 | 26 | 31 バイト以内 |

- scan 側は manufacturer data の company ID と magic で `ScanFilter` をかける
- advertise は non-connectable。GATT 接続は使わない

### 2.4 sighting の解決

受信した token を DID に解決するときの規則。

- `bt:` → SQLite の順で引き、見つからなければ捨てる
- 観測時刻が `[window_start - 120 秒, window_start + 900 + 120 秒]` の外なら捨てる（端末の時計ずれの許容幅）
- 自分自身の token、ブロック関係（どちらの向きでも）の相手、`users.deleted_at` のある相手は捨てる

## 3. BLE 実装の `PeerProximity`

| 操作 | Redis 操作 |
| --- | --- |
| `Observe(a, b, first, last, rssi)` | `sgt:{a}:{b}:{bucket}` を更新（bucket = `floor(last/600秒)`、first は最小・last と rssi は最大を保つ）。`ble:recent:{a}` に `ZADD b last` |
| `Mutual(a, b, at, within)` | `sgt:{b}:{a}:{k}` を `at ± within` を覆うバケット分 `MGET` し、区間が `at ± within` に重なるものがあれば真 |
| `Neighbors(u, within)` | `ble:recent:{u}` を `ZRANGEBYSCORE now-within now`。逆向き（u を観測した相手）は `pe:active` から引く（08 §3） |

`ble:recent:*` は Observe のたびに `ZREMRANGEBYSCORE -inf now-30分` で古い要素を落とす。

## 4. GPS（位置の取り込み、P0）

`PUT /api/v1/me/location`

```json
{ "lat": 35.0, "lon": 135.0, "accuracyM": 12.5, "recordedAt": "2026-10-17T03:12:45Z", "source": "gps", "isMock": false }
```

検証規則（満たさなければ 400 で、位置は保存しない）。

| 規則 | 値 |
| --- | --- |
| 緯度経度の範囲 | lat ∈ [-90, 90]、lon ∈ [-180, 180] |
| 精度 | `accuracyM ≤ 200` |
| 時刻 | `now - 120 秒 ≤ recordedAt ≤ now + 30 秒` |
| モック位置 | `isMock = true` は拒否（Android `Location.isMock()` をクライアントが送る） |
| 速度の妥当性 | 直前の位置からの移動速度が 300 km/h を超えたら拒否し、`audit_log` に記録する |

保存は `loc:{did}`（hash、TTL 30 分）と `geo:users`（`GEOADD`）。SQLite には書かない。
保存後、位置共有（07）とグループ入場通知（09 §6）のフックを非同期で呼ぶ。

### 4.1 GPS 実装の `ZonePresence`

```
Inside(u, z, maxAge):
  loc := HGETALL loc:{u}
  if loc なし or now - loc.ts > maxAge: return ErrStale   → API では 403 location_stale
  d := haversine(z.center, loc)
  return d <= z.RadiusM + min(loc.acc, z.RadiusM * 0.5)
```

精度の半分を上限に許容幅を足すのは、境界付近で GPS 誤差により判定が揺れるのを抑えるため。

## 5. パラメータ

| 名前 | 既定 | 意味 |
| --- | --- | --- |
| `BeaconWindow` | 15 分 | token の有効窓 |
| `BeaconClockSkew` | 120 秒 | 窓の前後の許容幅 |
| `BeaconIssueMax` | 24 時間 | 一度に発行する最大時間 |
| `SightingBucket` | 10 分 | `sgt:` のバケット幅 |
| `LocationMaxAccuracy` | 200 m | 受け付ける精度の上限 |
| `LocationTTL` | 30 分 | `loc:` の TTL |
| `LocationFresh` | 120 秒 | geofence 判定に使える位置の鮮度（参加・作成時） |
| `PresenceGrace` | 10 分 | 参加後の閲覧・投稿の判定に使える位置の鮮度（09 §4） |
| `MaxSpeedKmh` | 300 | 速度の妥当性チェック |
