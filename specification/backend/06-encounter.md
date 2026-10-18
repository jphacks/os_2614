# 06 すれ違い

## 1. 定義

- **sighting**: 端末 A が端末 B の beacon token を受信した、という一方向の観測
- **すれ違い（encounter）**: A→B と B→A の sighting が、互いに `MatchWindow`（10 分）以内に揃ったもの（draft「双方向で互いを認識した場合のみ」）
- 同じ 2 人の encounter は、間隔が `MergeGap`（30 分）以内なら 1 件にまとめる。すれ違い回数は encounter の件数で数える

## 2. クライアントの送信（P0）

native の scan は token ごとに `firstSeen`, `lastSeen`, `maxRssi`, `count` を端末内で集約する。
送信は次のいずれかで行う。

- FGS 稼働中は 5 分ごと
- 未送信が 50 件を超えたとき
- アプリが前面に来たとき

`POST /api/v1/encounters/sightings`

```json
{
  "items": [
    { "token": "base64url", "firstSeenAt": "2026-10-17T03:10:02Z", "lastSeenAt": "2026-10-17T03:12:40Z", "maxRssi": -71, "count": 14 }
  ]
}
```

- 1 リクエスト 500 件まで。`lastSeenAt` が 24 時間より前の項目は捨てる（オフライン中のバッファを 24 時間まで許す）
- レスポンスは `{ "accepted": 12, "dropped": 3 }` だけを返す。**どの token が誰だったか、成立したかは返さない**（公開遅延を破らないため）

## 3. サーバ処理

項目ごとに次を行う。

```
1. token を解決して peer の DID を得る（05 §2.4）。失敗したら dropped
2. maxRssi < RssiMin（-100 dBm）なら dropped
3. PeerProximity.Observe(me, peer, first, last, rssi)
4. PeerProximity.Mutual(me, peer, at=last, within=MatchWindow) が偽なら終了
5. pair = sort(me, peer)。lock:enc:{a}:{b} を取る
6. encounters から pair の行で
     last_seen_at >= first - MergeGap かつ first_seen_at <= last + MergeGap
   を満たすものを探す
   - あれば first/last/max_rssi を広げ、公開時刻を再計算する（§5）
   - なければ新規作成する。established_at = now、visibility と reveal_at は §5
7. ロックを解放する
```

- 4 の相互判定は、相手の送信が後から届いた場合にも成立するように、**後から届いた側の処理で**成立させる。どちらが先に送っても結果は同じ
- 1 リクエスト内では peer ごとに 3〜7 をまとめて 1 回だけ行う

## 4. 不正対策

| 脅威 | 対策 |
| --- | --- |
| 他人の token を録って別の場所で流す（リレー攻撃） | 成立には逆方向の観測も必要。token は 15 分で失効する。完全には防げないが、得られるのは「すれ違い 1 件」だけで、相手の位置などは漏れない |
| 大量の sighting で回数を水増し | レート制限（02 §5）。1 ユーザーあたり 1 日に成立する encounter を 2000 件までに制限する |
| 配布された token を集めて逆引きする | token → DID の対応は API で一切返さない。成立した encounter も公開時刻までは見えない |

## 5. 公開設定（P0）

### 5.1 ユーザー設定

| mode | 意味 | `encounter_delay_sec` |
| --- | --- | --- |
| `immediate` | 成立したらすぐ互いに見える | 使わない（0 扱い） |
| `delayed` | 一定時間後に見える（既定） | 60〜604800（1 分〜7 日）。UI のプリセットは 1 時間 / 6 時間 / 1 日 / 3 日 |
| `private` | 誰とすれ違ったかを見せない（回数の集計にだけ入る） | 使わない |

### 5.2 2 人の設定の合成

非対称を減らすため、厳しい方を採る（draft「長い方の設定が適応される」）。

```
if A.mode == private or B.mode == private:
    visibility = hidden, reveal_at = NULL
else:
    delay = max(delayOf(A), delayOf(B))          # immediate は 0
    visibility = pending
    reveal_at  = (delay == 0) ? established_at
                              : max(現在の reveal_at, last_seen_at + delay)
```

- delay が 0 でないときは `last_seen_at` を基準にする。同じ教室に長時間いる場合、基準が成立時刻だと一緒にいる間に公開されてしまい、遅延の意味がなくなるため
- encounter が延長されるたびに `reveal_at` を再計算する（後ろにしか動かない）

### 5.3 設定変更の反映

- **厳しくする変更**（公開を遅らせる、private にする）は、まだ `pending` の encounter にも即時に反映する
- **緩める変更**は、これから成立する encounter にだけ反映する。相手が前の設定を前提にしていた可能性があるため

### 5.4 公開処理（`encounter.Revealer`、1 分 tick）

`visibility='pending' AND reveal_at <= now` の行を `revealed` にし、両者の WebSocket に `encounter.revealed` を送る。
1 件ごとのプッシュ通知は送らない（12 のダイジェストで回数を知らせる）。

## 6. 閲覧 API（P0）

`GET /api/v1/encounters?cursor=`（revealed のみ、`established_at` の新しい順）

```json
{
  "items": [
    {
      "id": "0192...",
      "peer": { "did": "did:plc:...", "handle": "bob.bsky.social", "displayName": "Bob", "avatarUrl": "..." },
      "seenFrom": "2026-10-17T03:00:00Z",
      "seenUntil": "2026-10-17T03:15:00Z",
      "timesWithPeer": 4,
      "relation": "none"
    }
  ],
  "cursor": "..."
}
```

- `seenFrom` / `seenUntil` は 15 分単位に丸める（正確な時刻から人物を特定しにくくする）
- `timesWithPeer` は、その相手との revealed な encounter の件数
- `relation` は `none` / `requested`（自分が申請中）/ `incoming`（相手から申請あり）/ `friend`。ここから friend 申請ができる（07 §2）

`GET /api/v1/encounters/summary`（P1）

```json
{ "today": 12, "week": 48, "month": 160, "total": 1024, "uniquePeople": 380 }
```

- revealed に加えて pending / hidden も数える。ただし **成立から 1 時間以内のものは数えない**。回数がリアルタイムに増えると、周りの誰とすれ違ったかを推測できてしまうため
- 日・週・月の区切りは Asia/Tokyo（週は月曜始まり）

## 7. すれ違い場所の記録

- 両者が `record_encounter_place` を有効にした場合だけ、成立時に報告側の最新位置（`loc:`、鮮度 10 分以内）の geohash 6 桁（約 1.2 km × 0.6 km）を保存する
- 座標そのものは保存しない
- 設定項目は `user_settings.record_encounter_place`（既定 0）。`PATCH /me/settings` の `recordEncounterPlace` で変える
- 成立（新規作成）時だけ保存する。同じすれ違いへの統合（merge）では更新しない
- 位置がない・10 分より古い場合は場所なしのまま成立させる

## 8. パラメータ

| 名前 | 既定 | 意味 |
| --- | --- | --- |
| `MatchWindow` | 10 分 | 双方向の観測を同じすれ違いとみなす時間差 |
| `MergeGap` | 30 分 | 同じ 2 人の encounter をまとめる間隔 |
| `RssiMin` | -100 dBm | これより弱い観測は捨てる |
| `SightingMaxAge` | 24 時間 | 受け付ける観測の古さ |
| `DailyEncounterCap` | 2000 | 1 ユーザー 1 日あたりの成立上限 |
| `SummaryHoldback` | 1 時間 | summary に含めない直近の時間 |
| `TimeRounding` | 15 分 | 閲覧時の時刻の丸め |
