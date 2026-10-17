# 07 フレンドと位置共有

## 1. 方針

- draft どおり**ユーザー検索は作らない**。フレンドになる経路は、実際に会ったことを裏付けるものに限る
- フレンドであることは、位置共有（§4）と Party の自動作成（08）の前提になる
- フレンド数の上限は 500

## 2. フレンドになる経路（P0）

| 経路 | 条件 | 承認 |
| --- | --- | --- |
| 招待コード（QR） | 対面でコードを読み取る | 読み取った時点で成立（作成者と読み取り者の両方が操作しているため） |
| すれ違いから申請 | 相手との **revealed** な encounter がある | 相手の承認が要る |
| Space から申請 | 同じ Space（active / archived どちらでも）のメンバーだった | 相手の承認が要る |

上記の関係がない相手への申請は `403 forbidden`。

### 2.1 招待コード

`POST /api/v1/friends/invites` → `201`

```json
{ "code": "7KQ2-M9XD", "url": "chain://friend/7KQ2M9XD", "expiresAt": "2026-10-17T03:22:45Z" }
```

- 8 文字の Crockford Base32（約 40 ビット）。有効期限 10 分、1 回限り。DB には SHA-256 だけを保存する
- 同時に有効なコードは 1 ユーザー 1 個。新しく作ると古いコードは失効する

`POST /api/v1/friends/invites/{code}/accept`

- 期限切れ・使用済みは `410 gone`。自分のコードは `400`。ブロック関係は `403 blocked`。既にフレンドなら `409 conflict`
- 成功するとフレンドが成立し（`via='invite'`）、作成者に `friend.accepted` を通知する

### 2.2 申請

`POST /api/v1/friends/requests` `{ "did": "...", "via": "encounter" | "space", "viaRef": "encounter または space の ID" }`

- `viaRef` が §2 の条件を満たすかを確認する
- 相手から自分への pending な申請が既にあれば、新しい申請は作らずにその申請を承認する
- 相手に `friend.request` を通知する（12）

`POST /api/v1/friends/requests/{id}/accept`、`/decline`（受信者のみ）、`DELETE /api/v1/friends/requests/{id}`（送信者が取り消す）。
declined になった申請の送信者は、同じ相手に 7 日間は再申請できない。

### 2.3 一覧と削除

- `GET /api/v1/friends`: フレンドのプロフィールの要約と、`since`、自分が相手に設定している共有精度を返す
- `GET /api/v1/friends/requests?box=incoming|outgoing`
- `DELETE /api/v1/friends/{did}`: フレンド関係を消し、双方向の `location_shares` も消す。相手には通知しない

## 3. 状態遷移

```
          申請                承認
 (なし) ───────▶ pending ───────▶ friend
                   │  拒否 / 取消        │ 削除・ブロック
                   ▼                     ▼
               declined / canceled     (なし)
```

## 4. 位置共有（P1）

### 4.1 精度

draft の論点「位置情報の粒度をどうするか」には、相手ごとに精度を選べる形で答える。

| precision | 相手に見えるもの | 優先度 |
| --- | --- | --- |
| `exact` | 緯度経度（小数 5 桁 ≒ 1 m）と精度 | P1 |
| `approx` | geohash 6 桁のセルの中心（約 1.2 km × 0.6 km） | P1 |
| `area` | 自分が登録したエリアの label（「大学」など）。どのエリアにもいなければ「エリア外」 | P1（既定） |
| `direction` | 相手から見た 8 方位と距離帯（<100 m / <500 m / <2 km / <10 km / それ以上）。見る側の位置も 2 分以内のものが要る | P2 |
| `off` | 何も見えない | P1 |

- `areas_only = 1` にすると、自分が登録エリアのどれかにいるときだけ、その精度で見せる（draft「指定したエリアのみも可」、P2）
- 新しくフレンドになった相手への既定は `area`
- `PUT /api/v1/me/location-sharing {"paused": true}` で全員への共有を一時停止できる

### 4.2 API

- `PUT /api/v1/friends/{did}/location-share` `{ "precision": "area", "areasOnly": false }`
- `GET /api/v1/friends/locations`

```json
{
  "items": [
    { "did": "did:plc:...", "precision": "area", "area": { "label": "大学" }, "updatedAt": "2026-10-17T03:12:00Z" },
    { "did": "did:plc:...", "precision": "exact", "lat": 35.02613, "lon": 135.78089, "accuracyM": 12, "updatedAt": "..." },
    { "did": "did:plc:...", "precision": "off" }
  ]
}
```

- 位置は相手の `loc:{did}`（最長 30 分前まで）から、相手が自分に設定した精度で毎回計算して返す。保存はしない
- `updatedAt` は分単位に丸める
- 30 分以上更新のない相手は `"stale": true` とし、位置を返さない

### 4.3 エリア

- `GET / POST /api/v1/me/areas`、`PATCH / DELETE /api/v1/me/areas/{id}`
- 1 ユーザー 10 個まで、半径 50〜5000 m、label は 20 文字まで
- エリアの label は、area 精度で共有している相手にだけ見える

### 4.4 クライアントへの要求

- 常時の位置共有にはバックグラウンド位置（native）が必要（research §5）。backend はどこから来た位置かを区別しない
- 推奨の送信頻度: 前面では 30 秒ごと、バックグラウンドでは 5 分ごとか 100 m 移動ごと
