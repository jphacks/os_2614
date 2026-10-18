# 09 グループ

## 1. 定義

- 場所に結び付いた Space（`jp.i32.chain.space.group`）。流動的な人の集まりではなく、動かない空間で作られる（draft）
- 場所は円の geofence（中心と半径 5〜2000 m）で表す。判定には GPS を使う（教室では BLE が厳しいため。05 `ZonePresence`）
- 地図には出さない。グループ同士の包含関係をグラフとして返す（§7）

| 属性 | 値 | 意味 |
| --- | --- | --- |
| `visibility` | `open` / `closed` | open は付近の人が一覧から参加できる。closed は招待制 |
| `origin` | `explicit` / `natural` | ユーザーが作ったか、自然発生か（§8） |
| `persistent` | 0 / 1 | 1 なら、参加後はエリア外でも閲覧・投稿できる |
| `schedule` | 任意 | 有効な時間帯（§3）。指定がなければ常時有効 |

## 2. 作成（P0）

`POST /api/v1/groups`

```json
{
  "name": "情報科学演習 A",
  "description": "",
  "visibility": "open",
  "persistent": false,
  "geofence": { "lat": 35.0, "lon": 135.0, "radiusM": 40 },
  "schedule": { "tz": "Asia/Tokyo", "slots": [ { "weekday": 1, "start": "10:40", "end": "12:10" } ] },
  "inviteQuorum": 2,
  "force": false
}
```

- 作成者は geofence の中にいなければならない（`ZonePresence.Inside`、鮮度 `LocationFresh` 120 秒）
- name（40 文字）と description（500 文字）は moderation の対象（11）
- 作成者は `role=owner` のメンバーになる
- **重複の抑制**: draft の論点「教室で同じクラスに複数のグループが列挙される」への対応。active な open グループで、面積の重なり（IoU）が 0.8 以上かつ schedule の時間帯が重なるものがあれば、`409 conflict`（`details.similar` に候補）を返す。`force: true` なら作成する
- 作成後、`parent_space_id`（自分を完全に含む最小のグループ）を計算する。自分が含むグループ群の `parent_space_id` も再計算する

## 3. schedule

- `slots` は曜日（1=月〜7=日）と開始・終了時刻の組。最大 14 個
- `dates` は日付を指定した一回限りの時間帯（`{"date": "2026-10-17", "start": "10:40", "end": "12:10"}`）。最大 14 個。自動グループ（§8）はこちらを使う。`slots` と `dates` のどちらかに当てはまれば有効
- schedule のあるグループは、時間帯の外では nearby 一覧に出ず、参加もできない
- persistent でないグループは、時間帯の外では閲覧・投稿もできない。persistent なグループのメンバーは常に閲覧・投稿できる

## 4. アクセス判定（`checkUserAccess` 相当）

`instance.AccessChecker`（10 §5）がグループについて行う判定。

| mode | 条件 |
| --- | --- |
| `read` | メンバー（`left_at IS NULL` かつ `can_read`）であり、かつ次のどちらか: `persistent = 1`、または「schedule が有効」かつ「geofence の中にいる（鮮度 `PresenceGrace` 10 分）」 |
| `write` | `read` の条件に加えて `can_write` |
| メンバーでない人 | name、description、メンバー数、visibility だけが見える（open のみ）。投稿は見えない |

位置の鮮度を参加時（120 秒）より緩い 10 分にしているのは、教室内で GPS が途切れても閲覧できるようにするため。

## 5. 一覧と参加（P0）

`GET /api/v1/groups/nearby`

- 呼び出し元の最新位置（`loc:`、鮮度 120 秒）を使う。位置がなければ `403 location_stale`
- `GEOSEARCH geo:groups FROMLONLAT {lon} {lat} BYRADIUS 2000 m` で候補を取り、正確な包含を計算する
- 返すのは active で schedule が有効な **open** グループだけ。closed グループは出さない

```json
{
  "inside": [ { "id": "...", "name": "...", "memberCount": 23, "isMember": false, "persistent": false, "radiusM": 40 } ],
  "near":   [ { "id": "...", "name": "...", "memberCount": 5, "distanceBucket": "<500m" } ]
}
```

- `near` は 500 m 以内で、まだ中にいないグループ。距離は帯でだけ返す（地図化させないため）

`POST /api/v1/groups/{id}/join`

- open で、geofence の中にいる（120 秒）こと。違えば `403 not_in_geofence`
- 既にメンバーなら `409`。退出済みなら再加入させる

`POST /api/v1/groups/{id}/leave`。owner が退出した場合は、`joined_at` が最も古いメンバーを owner にする。最後のメンバーが退出したら archived にする。

## 6. closed グループの招待（P1）

draft「closed は n 名の招待が必要」を、メンバーの承認数で表す。

```
メンバーが招待を作る ── collecting ── 承認数 >= min(invite_quorum, メンバー数) ──▶ offered ── 対象者が承諾 ──▶ accepted（メンバーになる）
                          │ 7 日経過                                                  │ 拒否 / 7 日経過
                          ▼                                                           ▼
                       expired                                                  declined / expired
```

| 操作 | API | 権限・条件 |
| --- | --- | --- |
| 招待を作る | `POST /api/v1/groups/{id}/invites {"did"}` | 未退出のメンバー。対象は、招待者のフレンドか、招待者と revealed な encounter のある相手。既にメンバー・招待中・ブロック関係なら 409 / 403。招待者自身の承認を 1 として数える |
| 承認する | `POST /api/v1/groups/{id}/invites/{inviteId}/approve` | 招待者以外の未退出のメンバー。同じ人の 2 回目は 409 |
| 承認待ちの一覧 | `GET /api/v1/groups/{id}/invites` | メンバー。`collecting` の招待と、承認済みの人数、必要な人数 |
| 自分宛ての招待 | `GET /api/v1/group-invites` | 自分が対象の `offered` の招待。グループ名・招待者・期限 |
| 応答する | `POST /api/v1/group-invites/{inviteId}/accept`、`/decline` | 対象者。persistent でないグループでは、承諾時に geofence の中にいる必要がある（鮮度 120 秒） |

- 必要な承認数は、承認のたびに「その時点のメンバー数」で計算し直す（メンバーが減って条件を満たしたら、その時点で `offered` にする）
- 通知（12 §3）: 招待が作られたら、招待者以外のメンバーに `group.invite_approval`（承認のお願い）。`offered` になったら対象者に `group.invited`
- closed グループは、メンバー以外には nearby・グラフ・詳細のどこにも出さない（詳細は 404）
- 招待者は、`collecting` の間だけ招待を取り消せる（`DELETE /api/v1/groups/{id}/invites/{inviteId}`）。`offered` 以降は取り消せない
- 招待者が退出しても招待はそのまま残し、招待者の承認も 1 として数え続ける
- open と closed の切り替えは、owner の `PATCH /api/v1/groups/{id}`（`visibility`）か、投票（§8.6）で行う。owner のいない自動グループは投票だけ。closed から open に変えたら、`collecting` / `offered` の招待はそのまま有効にする（承諾すれば参加できる）

### 6.1 エリア入場通知（P1）

draft「オープンなグループではエリアに入った時に通知が来る」。

- `PUT /me/location` のたびに、その位置を含む active・open・schedule 有効のグループで、次のどれにも当てはまらないものを探す
  - 既にメンバー
  - 自分から退出したことがある（`space_members.left_at` がある）
  - `gn:{did}:{space}` がある（24 時間以内に通知済み）
- 見つかれば `group.nearby` を通知し、`gn:` を 24 時間立てる。1 回の位置更新で通知するのは 1 件まで（最も小さいグループ）
- 自動グループ（§8）は自動参加なので、この通知の対象にしない
- 入場をバックグラウンドで検知するにはバックグラウンド位置（native）が要る（README L2）。それまでは、アプリを開いて位置が届いたときにだけ通知される

## 7. グラフ API（P1）

draft「グループは大きさの異なる円」「地図よりグラフ表現」への対応。

`GET /api/v1/groups/graph`（呼び出し元の最新位置から 2 km 以内の、呼び出し元に見えるグループ）

```json
{
  "nodes": [ { "id": "g1", "name": "大学", "radiusM": 800, "memberCount": 120, "isMember": true, "inside": true, "origin": "explicit" } ],
  "edges": [
    { "from": "g1", "to": "g2", "kind": "contains" },
    { "from": "g2", "to": "g3", "kind": "overlaps" },
    { "from": "g2", "to": "g4", "kind": "shared_members", "weight": 6 }
  ]
}
```

- 「見える」とは、open か、自分がメンバーであること
- `contains`: `distance + r_child <= r_parent`。`overlaps`: `distance < r_a + r_b` で contains でないもの
- `shared_members` の `weight` は共通メンバーの人数だけで、誰が共通かは返さない。**2 人以上のときだけ出す**（1 人だと、自分以外の誰が両方にいるかを推測できるため）
- 中心座標は返さない（クライアントは radius をノードの大きさに使う）
- 呼び出し元の位置が古ければ `403 location_stale`

## 8. 自動グループ（P2）

draft「付近の人と calendar を参照して複数人がいれば作成」「calendar を使っていない人もアプリから今日の予定を入力できる」「投票で招待制に移行できる」。

### 8.1 方針

| 論点 | 決定 |
| --- | --- |
| 同じ予定の判定 | 予定のタイトルの **embedding（EmbeddingGemma など）の cos 類似度**と、時間の重なりで判定する。単語の一致ではなく意味の近さで比べる（「線形代数」と「線形代数学 II」を同じとみなせるように） |
| 送るもの | 予定のタイトルと時間を**そのまま**送る。サーバが embedding を計算する |
| 参加 | 条件を満たした人を**自動で参加させる**。予定を送ったこと（と設定の `natural_groups`）を参加の同意とみなす |
| セキュリティ | 予定のタイトルを平文で保存する点は、スケールしてから見直す（README L8） |

### 8.2 予定の送信

`PUT /api/v1/me/schedule-today`

```json
{ "date": "2026-10-17", "source": "calendar", "entries": [ { "title": "線形代数 II", "start": "10:40", "end": "12:10" } ] }
```

- `source` は `calendar`（端末のカレンダーから native が送る。`specification/android` §11）か `manual`（アプリでの手入力。WebView が送る）
- その日（Asia/Tokyo）の、**同じ `source` の予定だけ**をまとめて置き換える。`entries` が空ならその source の予定を消す。カレンダーの同期で手入力の予定が消えないようにするため
- `date` は今日か明日だけ。1 日 20 件まで（source ごと）、タイトルは 100 文字まで、`start < end`
- 照合では source を区別しない
- `GET /api/v1/me/schedule-today?date=` で自分の分を source 付きで返す。`DELETE /api/v1/me/schedule-today?date=&source=` で消す（source を省くと両方）
- 保存先は `schedule_entries`（04）。翌日以降に janitor が消す（今日と明日の分だけを持つ）
- `user_settings.natural_groups = 0` の人の予定は、照合に使わない（予定の保存はする）

### 8.3 embedding

```go
// Embedder は文の embedding を返す。返すベクトルは L2 正規化済み。
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
}
```

- 実装は、OpenAI 互換の `/v1/embeddings` を持つ推論サーバ（Ollama、llama.cpp server、Text Embeddings Inference など）を HTTP で呼ぶ。backend と同じマシンで動かす想定
- 設定: `CHAIN_EMBED_URL`、`CHAIN_EMBED_MODEL`（既定 `embeddinggemma`）、`CHAIN_EMBED_DIM`（既定 256。EmbeddingGemma は次元を切り詰めて使える）
- 入力は `task: clustering | query: {title}` の形にする（EmbeddingGemma の推奨プロンプト。モデルを替えたら見直す）
- 予定を受け取った時点で計算する（タイムアウト 3 秒）。失敗したら `embedding` を NULL のまま保存し、照合のワーカーが後で計算し直す
- ベクトルは `schedule_entries.embedding` に float32 のリトルエンディアンで保存し、`embed_model` を一緒に持つ。モデルが変わったら計算し直す

### 8.4 照合（`group.Clusterer`、5 分ごと）

```
1. 対象: 今その時間帯にいる予定（start - 10 分 ≤ now ≤ end）を持ち、
   natural_groups = 1 で、位置が新しい（geo:users、鮮度 5 分）ユーザー
2. 位置で塊にする: 50 m 以内を近いとみなす DBSCAN（最小 3 人）
3. 塊の中で、2 人の今の予定について
     cos(embedding) ≥ NaturalSimilarity（0.80）かつ 時間の重なりが短い方の 50% 以上
   の組をつなぐ
4. 3 人以上の連結成分ごとに:
   a. 既存のグループに合流できるか（§8.5）を先に見る
   b. できなければ候補として Redis に置く（キーは成分の重心の geohash 7 桁と予定の時間帯）。
      同じ候補が 3 回続けて（10 分）見つかったらグループを作る
```

### 8.5 作成と自動参加

- **既存のグループへの合流**: 成分の重心を含み、schedule の時間帯が重なる active な open グループ（明示的なものを含む）があり、成分のメンバーの予定とそのグループの代表の予定（§8.5 の名前の元）の類似度が条件を満たすなら、新しく作らずにそのグループへ自動参加させる
- **新しく作る場合**:
  - `origin = natural`、`visibility = open`、`persistent = 0`
  - geofence: メンバーの位置を囲む最小の円に 20 m の余裕を足す。半径は 30〜300 m に収める
  - schedule: `dates` に今日の 1 件（成分の予定の開始の最小〜終了の最大）
  - 名前: 成分の予定のうち、embedding が他の全員に最も近いもの（medoid）のタイトル。40 文字に切り詰める。moderation（11）にかけ、block なら「近くのグループ」にする
  - owner は置かない（全員 `member`。PATCH できる人がいない）
- **自動参加**: 成分の全員を `member` にし、`group.auto_joined` を通知する（「『線形代数 II』のグループに参加しました」。退出の導線を付ける）
- **あとから来た人**: 照合のたびに、既存の自動グループの geofence の中にいて、予定が条件を満たす人を自動参加させる
- **退出**: 通常の退出（§5）。自分から退出したグループには、同じグループが続く限り自動では戻さない（`left_at` があるか見る）
- **終了**: schedule の時間帯が終わってから 30 分、geofence の中にメンバーがいなければ archived にする（投票で persistent になったものを除く）。archived のグループは memory に出る（13 §2）

### 8.6 投票（P2）

draft「投票で招待制に移行ができる」。

- `POST /api/v1/groups/{id}/votes {"kind": "make_closed" | "make_persistent"}`: メンバーなら誰でも発議できる。同じ種類の投票が開いている間は 409
- `POST /api/v1/groups/{id}/votes/{voteId}/ballot {"yes": true}`
- 48 時間以内に、賛成が**その時点のメンバー数**の過半数に達したら可決。可決したら `visibility = closed` または `persistent = 1` にする。期限までに達しなければ否決
- 主な用途は、自動グループを「授業が終わっても残る、招待制のグループ」にすること

### 8.7 パラメータ

| 名前 | 既定 | 意味 |
| --- | --- | --- |
| `NaturalSimilarity` | 0.80 | 同じ予定とみなす cos 類似度。実データで調整する |
| `NaturalOverlap` | 0.5 | 時間の重なり（短い方に対する割合） |
| `NaturalClusterEps` | 50 m | 位置で塊にする距離 |
| `NaturalMinSize` | 3 | 作成に要る人数 |
| `NaturalConfirmTicks` | 3 | 作成までに続けて見つかる回数（5 分ごとなので 10 分） |
| `NaturalLocationFresh` | 5 分 | 照合に使う位置の鮮度 |
| `NaturalLeadTime` | 10 分 | 予定の開始の何分前から対象にするか |
| `NaturalEndGrace` | 30 分 | 時間帯の終了から archived までの猶予 |
| `NaturalRadius` | 30〜300 m | 自動グループの半径の範囲 |

## 9. 管理（P0〜P1）

| 操作 | 権限 | 優先度 |
| --- | --- | --- |
| `PATCH /api/v1/groups/{id}`（name / description / schedule / inviteQuorum） | owner | P0 |
| geofence の変更 | 不可（場所の意味が変わるため。作り直す） | — |
| `DELETE /api/v1/groups/{id}/members/{did}`（追放） | owner | P1 |
| `DELETE /api/v1/groups/{id}`（削除） | owner。`spaces.state = deleted` にし、誰も読めなくする | P1 |
| 他人の投稿の削除 | owner（10 §6） | P1 |

## 10. 位置偽装への対策

- モック位置の拒否と速度チェック（05 §4）
- 完全には防げない。グループは「その場にいる人の気軽な集まり」であり、位置の正しさに強い安全性を依存させる機能（入室の認証など）にはしない
