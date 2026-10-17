# CHAIN UI 仕様 v2

- 出典: Figma `CHAIN` (fileKey `7snAps9kalBUedZ3yFzxIj`) の `v2 / *` フレーム。**`v2 / Nearby` 系 (Nearby / Nearby (Bubbles) / Nearby (Menu Open)) は対象外**。
- `(Card)` 版がある画面は Card 版を正とする (灰色背景 + 白カード)。
- 参考スクリーンショット: `docs/spec/screens/*.png` (390×844)。ノード ID は §4.1 の Figma 列。
- 実装先: `web/` (Next.js 16, static export, Capacitor Android)。

## 1. 前提と制約

| 項目 | 決定 |
| --- | --- |
| レンダリング | `output: "export"`。サーバ機能 (Server Actions, cookies, Route Handlers, 動的ルート without `generateStaticParams`) は使わない |
| 画面 | ページは Server Component の薄い殻 + `'use client'` の画面コンポーネント |
| 可変 ID | 動的セグメントは使わず **クエリ文字列** (`/group/?id=g1`)。`useSearchParams` を使う画面は `<Suspense>` で包む |
| スタイル | **CSS Modules** (`*.module.css`) + `globals.css` の CSS 変数。Tailwind・CSS-in-JS は入れない。色・余白・角丸・文字はトークン経由で、生の hex を書かない |
| アイコン | `material-symbols` (Rounded) をセルフホスト済み。`<Icon name="hub" />` で使う |
| フォント | Noto Sans JP (`--font-noto-sans-jp`)。Figma のフォントは未確認のため仮 |
| 画像 | `<img>` を使う (`next/image` の最適化は static export で不可)。`@next/next/no-img-element` は該当行で無効化 |
| 対象幅 | 390px 基準のモバイル。360–430px で崩れないこと。横スクロール禁止 (メモリーのカルーセル以外) |
| backend | 未実装。UI は `src/domain/types.ts` の型だけに依存し、DTO → ドメイン変換は `src/data/mappers.ts` に閉じる (§5) |

## 2. デザイントークン

`src/app/globals.css` に定義済み。名前で参照すること。

### 2.1 色

| トークン | 値 | 用途 |
| --- | --- | --- |
| `--color-bg` | `#f5f5f5` | 画面背景、セグメントの地 |
| `--color-surface` | `#ffffff` | カード、タブバー |
| `--color-surface-muted` | `#efefef` | スライダーのトラック、未選択チップ |
| `--color-divider` | `#f2f2f2` | リスト区切り線 (左はアバター右端から) |
| `--color-placeholder` / `-strong` | `#e7e7e7` / `#d9d9d9` | 画像プレースホルダ、グラフの棒 |
| `--color-text` | `#3a3535` | 本文 |
| `--color-text-secondary` | `#8a8585` | 補足 |
| `--color-text-muted` | `#b3aeae` | 時刻、キャプション、非アクティブタブ |
| `--color-primary` | `#5b5555` | 主ボタン、FAB、選択中チップ、トグル ON、スライダーのつまみ側 |
| `--color-online` | `#6fcf97` | オンライン点、「いま」 |
| `--color-like` | `#e5736b` | いいね済みのハートと数 |
| `--color-notice-bg` | `#f4e4df` | エリア外の注意帯 |
| `--color-positive-bg` / `-text` | `#cfe6c9` / `#4f7a47` | 「先月より +38」バッジ |
| `--color-accent` / `-bg` | `#cf9aea` / `#f6edfb` | グループ作成画面 (アイコン枠、承諾率スライダー) |
| `--tone-*` | 8 色 | アバター背景 (`AvatarTone`) |
| `--color-scrim` | 黒 40% | ボトムシートの暗幕 |
| `--color-on-primary-subtle` / `-muted` | 白 15% / 70% | 濃色カード上の pill 地 / 補足文字 |

### 2.2 文字

| トークン | 指定 | 例 |
| --- | --- | --- |
| `--text-display` | 900 36px | 統計の「248」「60%」 |
| `--text-title` | 700 22px | 画面タイトル「友達」「情報工学科 3年」 |
| `--text-heading` | 700 16px | セクション見出し「いっしょにいる」 |
| `--text-body` | 400 15px | 投稿本文 |
| `--text-body-strong` | 700 15px | リストの名前 |
| `--text-caption` | 400 12px | 補足行、時刻 |
| `--text-micro` | 400 10px | タブラベル、スライダー目盛 |

`font: var(--text-heading);` のように shorthand で使う。

### 2.3 形と余白

- 角丸: カード `--radius-lg` (20px)、入力・サムネ `--radius-md` (12px)、チップ・ボタン・セグメント `--radius-pill`。
- 画面左右の余白 `--page-gutter` (20px)。カード内 padding 16px。セクション間 24px。
- 影は FAB のみ (`--shadow-fab`)。カードは影なし、背景との明度差で区切る。

## 3. コンポーネント

### 3.1 配置

```
src/
  domain/types.ts          … ドメイン型 (済)
  data/
    dto.ts                 … backend DTO の仮定義
    mappers.ts             … DTO → ドメイン
    mock.ts                … 画面確認用のモック (ドメイン型)
    format.ts              … 相対時刻・期間などの表示整形
  components/
    ui/                    … 汎用部品 (下表)。1 部品 = 1 ディレクトリ (Foo.tsx + Foo.module.css + index.ts)
    layout/                … AppShell, TabBar, ScreenHeader
    feature/<name>/        … 画面固有の部品 (post, friend, party, memory, stats, settings, compose, group-create)
    screens/               … 画面全体 (§4)。'use client'
  app/                     … ルート (§4.1)。page.tsx は screens/* を描画するだけ
```

### 3.2 汎用部品 (`components/ui`)

| 部品 | props | 見た目 |
| --- | --- | --- |
| `Icon` | `name: string; size?: number (既定 24); filled?: boolean; className?` | `<span className="material-symbols-rounded" aria-hidden>`。`filled` は `font-variation-settings: 'FILL' 1` |
| `Avatar` | `user: Pick<User,"name"\|"avatarUrl"\|"tone">; size?: 32\|40\|48\|56\|80; online?: boolean; ring?: boolean; dashed?: boolean` | 円。画像が無ければ tone 色の地に名前の先頭 1 文字 (500, size の 40%)。`online` は右下に 10px の緑点 (白 2px 縁)。`ring` は外側 2px `--color-tone-blue` 系の縁 (招待選択中)。`dashed` は「追加」用の点線円 |
| `AvatarStack` | `users: User[]; max?: number (既定 3); size?: 24` | 左に -8px ずつ重ねる。超過分は左側に `+n` (caption, muted) |
| `Button` | `variant: "primary"\|"secondary"\|"ghost"; size?: "md"\|"sm"; icon?: string; children` | pill。primary = `--color-primary` 地 + 白文字 700。secondary = `--color-surface-muted` 地。ghost = 地なし secondary 文字。md 高さ 48 / sm 高さ 36 |
| `IconButton` | `icon: string; label: string (aria-label); variant?: "plain"\|"filled"\|"surface"` | 36px 円。filled = primary 地 + 白、surface = 白地 |
| `Fab` | `icon?: "add"; label: string; onClick` | 56px 円、primary 地、`--shadow-fab`、画面右下 (右 16 / タブバー上 16) に fixed |
| `Card` | `as?: "section"\|"div"\|"li"; padding?: "none"\|"md"; children` | 白地 `--radius-lg`。左右 `--page-gutter` は親が持つ |
| `List` / `ListItem` | `ListItem: leading?: ReactNode; title; subtitle?; trailing?; href?; onClick?` | Card 内の縦リスト。行の高さ 64 (subtitle あり)。区切り線は leading の右端から右端まで |
| `SectionHeader` | `title; action?: { label; href?; onClick? }` | 左に heading、右に caption muted (「すべて見る ›」「近い順」) |
| `Chip` | `label; selected?; icon?; onClick?` | pill 高さ 28、caption 700。未選択 = 白地 (灰背景上) / `--color-surface-muted` (白背景上) |
| `Tag` | `label` | 白地 pill、caption 500 (「オープン」「18人」「GPSエリア」) |
| `SegmentedControl<T>` | `options: {value:T;label}[]; value: T; onChange` | 地 `--color-bg` (カード上) または `--color-surface-muted`、高さ 36、選択肢は白地 pill + 700 |
| `Toggle` | `checked; onChange; label (aria)` | 52×32。ON = primary 地 + 白つまみ右。`role="switch"` |
| `Slider` | `min; max; step; value; onChange; tone?: "primary"\|"accent"; marks?: string[]` | トラック 6px `--color-surface-muted`、塗り primary または accent、つまみ 24px 白 + 薄い影。`marks` は下に micro で両端・中央 |
| `Notice` | `icon?: string; children; tone?: "warning"\|"info"` | warning = `--color-notice-bg` 地、radius-md、caption。info = 地なし、muted + アイコン |
| `ImagePlaceholder` | `ratio?: string (既定 "4/3"); src?: string` | `src` があれば `<img>` cover、無ければ `--color-placeholder` 地に `image` アイコン (muted) |
| `StatTriple` | `items: {value: number\|string; label}[]` | 白カード内で 3 等分、間に縦線。値は 700 20px、ラベル caption muted。数値は `toLocaleString("ja-JP")` |
| `Badge` | `label; tone: "positive"` | pill、`--color-positive-bg` / `-text`、caption 700 |

### 3.3 レイアウト (`components/layout`)

- `AppShell`: `{ tab?: TabKey; children }`。上 safe-area + 本文 + `TabBar` (tab 指定時のみ)。本文下に `--tab-bar-height` の余白。
- `TabBar`: 4 タブ固定。`ちかく` (hub, `/`)、`パーティ` (celebration, `/party/`)、`友達` (group, `/friends/`)、`プロフィール` (person, `/profile/`)。白地、上に 1px divider、アイコン 26 + ラベル micro。選択中は `--color-text` + 700 + filled、他は `--color-text-muted`。下に safe-area。
- `ScreenHeader`: `{ title; subtitle?; back?: boolean | string; trailing?: ReactNode; count?: string }`。戻るは `arrow_back_ios_new` (back が string ならそこへ、true なら `history.back()`)。タイトル `--text-title`、`count` はタイトル横に caption muted (「8人」)。

## 4. 画面

### 4.1 ルート

| パス | 画面 | タブ | Figma |
| --- | --- | --- | --- |
| `/welcome/` | Welcome | なし | 200:2 |
| `/` | ちかく (**対象外**。「準備中」の空画面だけ置く) | ちかく | — |
| `/party/` | Party | パーティ | 200:45 |
| `/group/?id=` | Group | ちかく | 216:1687 |
| `/group/replies/?id=&post=` | Group (Reply) | なし | 213:1196 |
| `/group/new/` | Create Group | なし | 216:2038 |
| `/compose/` | Compose | なし | 213:1059 |
| `/friends/` | Friends | 友達 | 216:1570 |
| `/profile/` | Profile | プロフィール | 210:385 |
| `/memory/` | Memory | プロフィール | 200:47 |
| `/stats/` | Stats | なし | 210:460 |
| `/settings/` | Settings | なし | 216:1643 |

ステータスバー・ホームインジケータ・キーボードは OS のものなので描かない。

### 4.2 Welcome (`/welcome/`)

白背景。四隅に `--color-bg` 系の大きな雲シルエット (装飾、SVG 円の重ね合わせで可、`aria-hidden`)。中央上寄りに灰色 (`--color-placeholder-strong`) の雲キャラ (白い目 2 つ) と、その下に `CHAIN` (900 34px, `--color-primary`, 字間 0.04em)。下部に縦並びボタン 2 つ (幅 256、間 16):
1. primary「Blueskyで続ける」— 左端に白い 36px 円 (ロゴ置き場)。
2. 無効風 secondary「メールアドレス」— 左に `mail` アイコン、`--color-placeholder-strong` 地 + 白文字。

### 4.3 Group (`/group/?id=`)

- ヘッダー: 戻る + グループ名 (`--text-title`) + 右に `more_horiz`。下に `Tag` 横並び: 公開範囲 (オープン/招待制)、`{memberCount}人`、`{areaLabel}`。
- `insideArea=false` のとき `Notice tone=warning icon=location_on`「エリアに入りました・エリア外では投稿できません」を表示 (文言は状態で切替: 外 →「エリア外では投稿できません」)。
- `SegmentedControl`: タイムライン / ピン / メディア / メンバー (タイムラインのみ実装、他は空状態)。
- タイムライン: 先頭に pinned 投稿を `PinnedPostCard` (`push_pin` + 「自動ピン・{author}」caption muted、本文 body-strong)。以降 `PostCard` を縦に 12px 間隔。
- `PostCard`: 白カード。左 Avatar 32、右に 名前 (body-strong 14px) + 時刻 (caption muted, `HH:mm`)、本文、画像があれば `ImagePlaceholder` (radius-md, 幅いっぱい)、下に `chat_bubble` + replyCount、`favorite` + likeCount (likedByMe なら `--color-like` + filled)。返信アイコン押下で `/group/replies/?id=&post=`。
- `Fab` → `/compose/?target=group:{id}`。`insideArea=false` なら非表示。

### 4.4 Group (Reply) (`/group/replies/`)

Group のタイムラインの上に被さるボトムシート (背面は `--color-scrim`、上端 radius-lg、上に 36×4 のグラバー)。シート上部: 中央に「返信」heading、右に `close`、下に divider。元投稿 (Avatar 40 + 名前・`・HH:mm` + 本文 + 「返信 {n}件」caption muted) → 返信リスト (Avatar 40、名前 body-strong・`・HH:mm` muted、本文、`favorite` + 数。likedByMe なら `--color-like`)。各行の間は divider (全幅)。元投稿のアバター下端から最初の返信のアバター上端まで 2px の `--color-divider` より濃い縦線 (`--color-placeholder-strong`) でつなぐ。下端に固定の入力欄: 上に「{name} に返信」caption、Avatar 32 (自分) + pill 入力「返信を書く…」+ 右に primary 円ボタン `arrow_upward`。

### 4.5 Party (`/party/`)

- タイトル「パーティ」(`--text-title`) + 補足「{自分以外の members の名前を・で連結} と合流しました」(caption muted)。
- **ヒーローカード (濃色)**: `--color-primary` 地、radius-lg、padding 16、文字は白系。
  - 左上: `--color-on-primary-subtle` の pill に `--color-online` の 8px 点 +「本パーティ」(caption 700)。stage=provisional なら「仮パーティ」。右上: areaLabel (caption, `--color-on-primary-muted`)。
  - 「いっしょにいる時間」(caption, `--color-on-primary-muted`) + 経過時間 `HH:MM:SS` (display, 白、tabular-nums。1 秒ごと更新、`startedAt` から算出)。
  - 下端に進捗バー: 高さ 4 の白 pill トラック上に 12px の白丸 (primary 2px 縁) のつまみ。値は仮→本パーティの進み (provisional は 30 秒→5 分の比率、active は先頭付近に固定)。操作不可・装飾 (`role="progressbar"`)。
- 行カード (白): `push_pin` +「パーティを固定」(body-strong) + 補足「旅行のときなど、はぐれても解散しません」(caption muted) + 右に `Toggle` (`fixed`)。Toggle OFF は `--color-placeholder-strong` 地 + 白つまみ左。
- 「メンバー {n}」見出し + 右に micro muted「点線 ＝ フレンドのフレンド」。Avatar 48 を横並び (横スクロール可)、名前を下に caption。`viaFriend` のメンバーは **白地 + `--color-placeholder-strong` の点線縁 + muted 文字** (`Avatar` の `dashed` を流用)。
- 「共有された写真」見出し + 右に「{photoCount}枚」。3 列グリッド (間 4、比率 6/5、radius-md、`ImagePlaceholder`)。各写真の右下 (6px 内側) に投稿者 `by.tone` の 14px 円 (白 2px 縁)。最大 6 枚、6 枚目は超過数 `+n` (heading 20px 白) を `--color-overlay-count` 地で全面に重ねる。
- 写真グリッドの下にタイムライン: Group と同じ `PostCard`（新しい順、「もっと見る」）。lock / unlock の system 投稿は中央寄せの 1 行 (caption muted)。返信は `/party/replies/?id=&post=`（Group (Reply) と同じシート）。Pin は出さない。
- `Fab` → `/compose/?target=party:{id}`。
- 仮パーティ (phase=tentative) の間: `Fab` と名前の変更を出さない。固定の `Toggle` は押せない状態にし、補足を「本パーティになると固定できます」にする。写真とタイムラインの代わりに「本パーティになると、写真や投稿を共有できます」。退出は出す。
- パーティが無いとき: 空状態「近くのフレンドと会うとパーティが始まります」。終了の直後は、上に `Notice`「パーティが終わりました。メモリーに残っています」を出す。
- 詳しい振る舞いは `docs/spec/party-implementation.md` §6。

### 4.6 Friends (`/friends/`)

- ヘッダー: 「友達」+ count「{n}人」、補足「位置はエリア名だけ共有しています」、右に `IconButton variant=filled icon=person_add`。
- 「いっしょにいる」: together=true の友達を Card + List。Avatar 40 (online)、名前、subtitle = locationLabel、trailing「いま」(`--color-online`, caption)。
- 「みんな」+ 右「近い順」: 残りを List。subtitle の頭にアイコン: area → `location_on`、direction → `explore`、hidden → `location_off` +「位置を見せていません」。trailing = 相対時刻 (`format.ts` の `relativeTime`: 「10分前」「1時間前」「きのう」)。
- 末尾に caption muted「検索はありません。すれ違ったことのある人とだけ友達になれます」。

### 4.7 Profile (`/profile/`)

- バナー (高さ 150, radius-lg, `--tone-blue` 系の地 + 白い雲装飾) 右上に `IconButton surface icon=edit`。
- Avatar 80 (白 4px 縁) をバナー左下に半分重ねる。右側に secondary sm ボタン「編集」(icon edit)。
- 名前 (`--text-title`) + 右にステータス pill (白地、online 点 + status)。bio (body, secondary)。
- `StatTriple`: すれ違い / 友達 / パーティ。
- 「メモリー」+「すべて見る ›」(→ `/memory/`)。横スクロールのカード (幅 150): ImagePlaceholder + タイトル body-strong + 「{期間}・写真 {n}枚」caption muted。
- Card + List: `tune` 設定 (→ `/settings/`)、`insights` これまでの統計 (→ `/stats/`)。trailing は `chevron_right`。

### 4.8 Memory (`/memory/`)

- ヘッダー: 戻る + 「メモリー」、補足「終わったパーティは、ここに残ります」。
- `StatTriple` (Profile と同じ値)。
- 「アーカイブ」+ 右「新しい順」。`MemoryCard` を縦に: 上に 4 枚のサムネ (正方形、間 8、4 枚目は `+{photoCount-3}` 重ね)、下に タイトル heading + 「{M/D}–{M/D}・{期間}・{n}人・写真 {k}枚」caption muted、右に `AvatarStack`。

### 4.9 Stats (`/stats/`)

- ヘッダー: 戻る + 「これまでの統計」。`SegmentedControl` 週 / 月 / 年 / すべて (`StatsRange`)。
- すれ違いカード: 「すれ違い」heading + 右上 `Badge`「先月より +{delta}」(期間に応じ 先週/先月/去年、all はバッジなし)。値 display + 小さく「・10月」。棒グラフ: 高さ 90、棒は等幅・角丸 4、色 `--color-placeholder-strong`、最後 (今日) だけ `--color-primary`。下に最初と最後のラベル (micro muted)。軸・目盛線なし。棒に `aria-label`。
- 2×2 の小カード: 友達 `{n}人` / 「今月 +{d}人」、パーティ `{n}回` / 「合計 {h}時間」、よくすれ違う時間 / peakHourNote、よくいるエリア / 「すれ違いの {ratio}%」。値は 700 22px。
- 活発度カード: 左に 12×7 の小さな正方形グリッド (10px, 間 4, 段階 0–4 を `--color-surface-muted`→`--color-primary` の 5 段階)、右に「今月のメッセージ」+ 値。下に caption muted「直近12週・パーティとグループでの発言」。

### 4.10 Settings (`/settings/`)

- ヘッダー: 戻る + 「設定」。
- 「すれ違いの公開」+ 補足「すれ違った相手が、あなたを見られるタイミング」。カード: 「相手に見えるまで」+ 右に現在値 (body-strong: 「すぐ」「3時間」「1日」「非公開」)。`Slider` 目盛 すぐ / 1日 / 非公開。段階は `[0, 1, 3, 6, 12, 24, 72, null]` 時間 (null = 非公開) の index。下に `Notice info icon=info`「相手と設定がちがうときは、長いほうが使われます」。
- 「友達申請」カード: 「友達申請を許可」+ 補足「オフにすると、新しい申請は届きません」+ `Toggle`。
- 「友達への位置の見せ方」カード: `Chip` 3 つ等幅 (エリア名 / 方角だけ / 見せない)、区切り、「決めたエリアだけで共有」+ 補足 `sharedAreas` を「・」連結 +「の{n}か所」+ `Toggle`。
- 末尾 `Notice info icon=lock`「DMはありません。会話はパーティとグループの中だけ」中央寄せ。
- 変更は親から渡す `onChange(settings)` に通知する (保存処理は持たない)。

### 4.11 Compose (`/compose/`)

白背景。上部: 左「キャンセル」(ghost)、右 primary sm「投稿する」(本文空なら disabled)。
- Avatar 40 (自分) + 投稿先セレクタ pill (`hub` 系アイコン + 名前 + `expand_more`)。
- 本文 textarea (枠なし、body 17px、自動伸長、上限 140 字)。
- 添付画像: 160px 正方形 radius-md、右上に暗い円の `close`。
- 「送信先」caption + `Chip` 横並び (`PostTarget[]`、選択中 primary)。下に caption muted「グループのエリアにいる {n}人 のタイムラインに表示されます」(party は「パーティのメンバーに表示されます」)。
- 下部ツールバー (上に divider): `image` `photo_camera` `location_on` `checklist` の IconButton、右に「あと {140-len}」caption + 進捗リング (24px SVG, primary)。
- 最下部 caption muted + `verified_user`「投稿前に、誹謗中傷にならないかAIがチェックします」。
- props: `targets: PostTarget[]; initialTargetId?; onSubmit(draft: { targetId; body; attachments: File[] })`。

### 4.12 Create Group (`/group/new/`)

白背景、セクション間は 8px の `--color-bg` 帯で区切る。
- ヘッダー行: 「キャンセル」/ 中央「グループを作る」heading / 右 primary sm「作成」(名前空で disabled)。
- アイコン選択: 80px 円、`--color-accent-bg` 地 + `--color-accent` の点線縁 + `add_a_photo`。右に「グループ名」caption + 下線入力 (heading 18px)、右下に「{len}/30」。
- 「公開範囲」: ラジオ 2 行。左に 36px 円アイコン (open: `public` / `--color-notice-bg` 地、closed: `lock` / `--color-accent-bg` 地)、タイトル body-strong + 補足 caption、右にラジオ (選択中は primary の二重丸)。
  - オープン「エリアにいる人がリストから自由に参加できます」
  - クローズド（招待制）「招待された人が承諾するとグループができます」
- closed のときだけ「承諾が必要な割合」: 補足「招待した人のうち、この割合が承諾するとグループが作られます」、値 display (「60%」) + 「{n}人中 {k}人が承諾すれば作成」、`Slider tone=accent` 目盛 1人 / 50% / 全員。
- 「招待する人」+ 右に「{n}人」。Avatar 48 横並び + 名前、選択中は ring。末尾に dashed Avatar「追加」。
- 末尾 `Notice info icon=location_on`「グループは今いるエリア（{areaLabel}）に作られます」。
- props: `candidates: User[]; areaLabel; onSubmit(draft: { name; visibility; approvalRatio; inviteeIds; icon?: File })`。

## 5. データ層 (backend 未確定への備え)

1. UI (components/screens) は **`src/domain/types.ts` だけ** を import する。DTO を UI に持ち込まない。
2. `src/data/dto.ts` に backend レスポンスの仮型を置く。命名は snake_case (Go/Fiber の JSON を想定)、時刻は ISO 文字列、任意項目は `?:`。
3. `src/data/mappers.ts` にドメイン型ごとの `toUser(dto)`, `toFriend`, `toGroup`, `toPost`, `toReply`, `toParty`, `toMemory`, `toStats`, `toSettings` と逆方向 `fromSettings(settings)` を置く。純関数、例外を投げない (欠損は既定値)。`avatarToneFor(id)` で ID から tone を決める (文字コード和 mod 8)。
4. backend が確定したら dto.ts と mappers.ts だけを直す。
5. 画面は props でデータを受ける。`app/*/page.tsx` が `src/data/mock.ts` の値を渡す (取得処理は後で `src/data/api.ts` を足して差し替える)。
6. `src/data/format.ts`: `relativeTime(ts, now)`, `clockTime(ts)` (`HH:mm`), `elapsed(startTs, now)` (`HH:MM:SS`), `dateRange(start, end)` (`10/3–10/4`), `durationLabel(start, end)` (`2日間` / `3時間`)。

## 6. 受け入れ条件

- `pnpm lint` と `pnpm build` (static export) が通る。
- 各ルートが mock で描画され、§4 の要素が揃っている。
- 生の hex 値が `globals.css` 以外に無い (`grep -rE '#[0-9a-fA-F]{3,6}' src --include=*.css --include=*.tsx` が globals.css のみ)。
- ボタン・トグル・セグメントはキーボード操作可能で、アイコンだけのボタンに `aria-label` がある。
