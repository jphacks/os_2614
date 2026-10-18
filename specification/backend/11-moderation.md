# 11 moderation（Jev / NanoGPT Decisions API）

## 1. 方針

draft の議論（「言論の自由の侵害では」→「誹謗中傷は言論の自由ではない」）を、次の 2 段階で実装に落とす。

| label | 意味 | 表示 | ユーザーが変えられるか |
| --- | --- | --- | --- |
| `block` | 誹謗中傷・脅迫・個人情報の暴露など | 作成を拒否する。後から判定された場合は非表示 | 変えられない |
| `warn` | 攻撃的・性的・センシティブだが、禁止まではしないもの | `warn_display` に従う（`show` / `blur`（既定）/ `hide`） | 変えられる |
| `ok` | 問題なし | そのまま | — |

「検閲を切れるようにすべき」という意見には `warn` の表示設定で応える。`block` は切れない。

## 2. 対象

| 対象 | 判定の時点 | 優先度 |
| --- | --- | --- |
| instance の投稿 | 作成前（同期） | P0 |
| グループの name / description、Party の name | 作成・変更前（同期） | P0 |
| 公開投稿（CHAIN から作るもの） | PDS に書く前（同期） | P1 |
| 公開投稿（Jetstream から取り込んだもの） | 取り込み後（非同期） | P1 |
| プロフィールのステータス | 変更前（同期）。Jetstream 経由の変更は非同期 | P1 |
| 画像 | P2（§5.6） | P2 |

## 3. 処理の流れ

```
作成要求
  └─ Moderator.Judge（タイムアウト 2 秒）
       ├─ block   → 422 moderation_blocked。何も保存しない。audit_log に記録
       ├─ warn    → mod_label=warn で保存
       ├─ ok      → mod_label=ok で保存
       └─ エラー / タイムアウト → mod_label=ok, mod_state=pending で保存し、modq に積む

moderation ワーカー（modq を消費）
  └─ 再判定。最大 5 回、指数バックオフ（10 秒〜）
       ├─ block → mod_label=block（非表示）。投稿者に moderation.hidden を通知
       └─ その他 → mod_label を更新し、mod_state=done
```

- 判定が取れない間は投稿を止めない（Jev の障害でアプリ全体が使えなくなるのを避ける）。そのため、`pending` の間は一時的に表示される
- 422 の応答には `details.categories`（Jev が返す分類）を入れ、どこが問題だったかをユーザーに示せるようにする

## 4. 表示の指示

一覧 API の各要素に次を付け、クライアントはこれに従って描画する。

```json
{ "moderation": { "label": "warn", "display": "blur", "categories": ["harassment"] } }
```

`display` は、`label` と閲覧者の `warn_display` から backend が決める。`hide` のものはそもそも返さない。

moderation の結果は Space の外（公開ラベラー）には出さない（03 §7.1）。

## 5. Jev（NanoGPT Decisions API）

### 5.1 呼び出し

| 項目 | 値 |
| --- | --- |
| エンドポイント | `POST {CHAIN_NANOGPT_URL}`（既定 `https://api.nano-gpt.com/api/v1/decisions`） |
| 認証 | `Authorization: Bearer {CHAIN_NANOGPT_KEY}` |
| モデル | `{CHAIN_JEV_MODEL}`（既定 `typesafe/jev-1.13`。`jev-latest` は判定が予告なく変わるので使わない） |
| タイムアウト | 同期判定は 2 秒、再判定ワーカーは 10 秒 |
| `include_usage` | 送らない（`false`）。`true` にすると課金確定まで応答が遅れる |
| 本文の上限 | JSON 全体で 1 MiB、文脈長 32K トークン。投稿は最大 2000 字なので超えない |

Decisions API は文章を生成せず、質問ごとに確率を返す。1 回の呼び出しで複数の質問を投げられるので、instance の投稿では **moderation と Pin を 1 回で判定する**（遅延と費用を半分にするため）。

### 5.2 リクエスト

```json
{
  "model": "typesafe/jev-1.13",
  "state": {
    "kind": "instance_post",
    "text": "明日の集合は 9:00 正門。遅れる人は連絡して"
  },
  "questions": {
    "safety": {
      "type": "choice",
      "instructions": "SNS の投稿として、この文章をどう扱うべきか。日本語の文脈とスラングを考慮すること。",
      "criteria": {
        "ok": "問題のない内容",
        "warn": "攻撃的・性的・暴力的・センシティブだが、特定の個人や集団への加害ではない内容",
        "block": "特定の個人や集団への誹謗中傷・脅迫・差別、個人情報の暴露、自傷や犯罪の扇動"
      }
    },
    "category": {
      "type": "choice",
      "instructions": "最も当てはまる問題の種類",
      "criteria": {
        "none": null, "harassment": null, "hate": null, "sexual": null,
        "violence": null, "self_harm": null, "personal_info": null, "spam": null
      }
    },
    "pin": {
      "type": "noul",
      "instructions": "グループのメンバーが後で見返す必要のある情報（日時、場所、持ち物、締切、連絡事項）を含むか"
    }
  }
}
```

- `state` には本文と投稿の種類だけを入れる。**DID・handle・Space 名・他の投稿は送らない**（02 §8）。`user` と `session_id` も送らない
- `pin` の質問は instance の投稿のときだけ付ける。それ以外（公開投稿、グループ名など）は `safety` と `category` だけ
- `instructions` と `criteria` の文言は `moderation/prompts.go` に定数で置き、変えたら版番号を上げる（判定結果の比較のため `records` に版を残す必要が出たら列を足す）

### 5.3 応答の解釈

```json
{
  "answers": {
    "safety":   { "type": "choice", "choice": "ok", "confidence": 0.97, "probabilities": { "ok": 0.97, "warn": 0.02, "block": 0.01 } },
    "category": { "type": "choice", "choice": "none", "confidence": 0.95, "probabilities": { "none": 0.95 } },
    "pin":      { "type": "noul", "noul": 0.88 }
  }
}
```

`choice` をそのまま使わず、確率にしきい値をかける。値は仮置きで、日本語の評価用データで調整する（README Q10）。

| 判定 | 条件 |
| --- | --- |
| `block` | `p(block) >= 0.70` |
| `warn` | block でなく、`p(block) + p(warn) >= 0.60` |
| `ok` | それ以外 |
| Pin する | `pin.noul >= 0.75` |

- `Verdict.Categories` には、`category` の確率が 0.3 以上の種類（`none` を除く）を入れる
- `Verdict.Score` は `p(block) + p(warn)`

### 5.4 エラー

| 応答 | 扱い |
| --- | --- |
| タイムアウト・5xx・`429` | §3 の「エラー」として `pending` で保存し、再判定する |
| `400`（質問・モデルの指定誤り） | こちらの不具合。`pending` で保存し、エラーログを出す（再判定しても直らないので、修正後に `modq` を流し直す） |
| `401` | 鍵の誤り・失効。`pending` で保存し、エラーログを出す |
| `413` | 起きない想定（本文の上限で防ぐ）。起きたら `block` ではなく `pending` とする |

### 5.5 interface

```go
type Label string

const (
	LabelOK    Label = "ok"
	LabelWarn  Label = "warn"
	LabelBlock Label = "block"
)

type Content struct {
	Kind string // "instance_post" | "public_post" | "nearby_post" | "group_name" | "party_name" | "status"
	Text string
}

type Verdict struct {
	Label      Label
	Categories []string
	Score      float64
}

type Moderator interface {
	Judge(ctx context.Context, c Content) (Verdict, error)
}

// instance の投稿用。moderation と自動 Pin（10 §6）を 1 回の呼び出しで判定する。
type PostAssessor interface {
	Assess(ctx context.Context, c Content) (v Verdict, pin bool, err error)
}
```

- 実装は `moderation.Jev`（両方の interface を満たす）。`CHAIN_NANOGPT_KEY` が未設定なら、常に `ok` / `false` を返す `moderation.Noop` を使う
- テスト用に、特定の語を含むと `block` / `warn` を返す `moderation.Fake` を用意する
- Pin の判定を投稿作成と同期で得られるので、10 §6 の「非同期で `PinClassifier` にかける」は、`Assess` の結果で Pin を作る処理に置き換える。判定が `pending` になった投稿は、再判定ワーカーで Pin も判定する

### 5.6 画像（P2）

- `typesafe/jev-1.13` は画像を受け付けない。画像の判定には画像対応モデルを使う。候補は `perplexity/pplx-decider-v1.1-27b`（zero data retention を明記。ホストは `https://nano-gpt.com` の方だけが対応）
- 画像は長辺 512 px に縮小した JPEG を base64 の data URL で `state` の配列に入れる（1 リクエスト 4 枚まで）
- `cloudflare/clef` 系は zero data retention に対応しないため、Space 内の写真には使わない

## 6. 通報（P2）

- `POST /api/v1/reports {"uri": "...", "reason": "..."}`
- グループの owner と運営が確認できる一覧を作る。P2 着手時に表を足す
