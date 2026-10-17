# 09 グループ

## 1. 定義

- 場所に結び付いた Space（`jp.i32.chain.space.group`）。流動的な人の集まりではなく、動かない空間で作られる（draft）
- 場所は円の geofence（中心と半径 20〜2000 m）で表す。判定には GPS を使う（教室では BLE が厳しいため。05 `ZonePresence`）
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

- `POST /api/v1/groups/{id}/invites {did}`: 招待者自身の承認を 1 として数える。対象は、招待者のフレンドか、招待者と revealed な encounter のある相手に限る
- `POST /api/v1/groups/{id}/invites/{inviteId}/approve`: 他のメンバーが承認する
- `offered` になったら対象者に `group.invited` を通知する
- `POST /api/v1/group-invites/{inviteId}/accept`、`/decline`: 対象者が応答する。persistent でないグループでは、承諾時に geofence の中にいる必要がある

### 6.1 エリア入場通知（P1）

draft「オープンなグループではエリアに入った時に通知が来る」。

- `PUT /me/location` のたびに、その位置を含む active・open・schedule 有効のグループのうち、まだメンバーでなく、`gn:{did}:{space}` がないものを探す
- 見つかれば `group.nearby` を通知し、`gn:` を 24 時間立てる。1 回の位置更新で通知するのは 1 件まで（最も小さいグループ）
- バックグラウンドで入場を検知するにはバックグラウンド位置（native）が要る。backend は位置が届いた時点で判定するだけ

## 7. グラフ API（P1）

draft「グループは大きさの異なる円」「地図よりグラフ表現」への対応。

`GET /api/v1/groups/graph`（呼び出し元の最新位置から 2 km 以内の、呼び出し元に見えるグループ）

```json
{
  "nodes": [ { "id": "g1", "name": "大学", "radiusM": 800, "memberCount": 120, "isMember": true, "inside": true } ],
  "edges": [
    { "from": "g1", "to": "g2", "kind": "contains" },
    { "from": "g2", "to": "g3", "kind": "overlaps" },
    { "from": "g2", "to": "g4", "kind": "shared_members", "weight": 6 }
  ]
}
```

- 「見える」とは、open か、自分がメンバーであること
- `contains`: `distance + r_child <= r_parent`。`overlaps`: `distance < r_a + r_b` で contains でないもの
- `shared_members` の `weight` は共通メンバーの人数だけ。誰が共通かは返さない
- 中心座標は返さない（クライアントは radius をノードの大きさに使う）

## 8. 自然発生グループ（P2）

draft「付近の人と calendar を参照して cos 類似度などで plot し、複数人がいれば作成」。

- **入力**: 同意したユーザーだけが、今日の予定を時間帯とキーワードで送る（`PUT /api/v1/me/schedule-today`）。予定の本文は送らない（キーワードはクライアントが抽出する）。カレンダーを使っていない人もアプリ内で入力できる。保存する表（`schedule_slots`、当日分のみ保持）は P2 着手時に `schema.sql` に足す
- **抽出**（`group.Clusterer`、5 分 tick）:
  1. `geo:users` の位置（鮮度 2 分）を 50 m で DBSCAN 的にまとめる（最小 3 人）
  2. 塊の中で、現在の時間帯の予定キーワードの TF ベクトルどうしの cos 類似度が 0.6 以上の組をつなぐ
  3. 3 人以上の連結成分が 10 分続いたら、その成分の外接円を geofence とする open・非 persistent のグループ候補を作る
- **参加は自動にしない**。候補を `group.suggested` で通知し、参加ボタンを押した人だけをメンバーにする
- 候補は 30 分誰も参加しなければ消す。メンバーが全員いなくなってから 30 分で archived にする
- **投票**（`group_votes`）: メンバーは `make_closed`（招待制に移る）と `make_persistent` を発議できる。48 時間以内に賛成がメンバーの過半数に達すれば可決

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
