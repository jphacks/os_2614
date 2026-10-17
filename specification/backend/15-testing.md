# 15 テスト方針

開発用の偽装 API（README L1）はここでは扱わない。

## 1. テストの層

| 層 | 対象 | 依存 | 実行 |
| --- | --- | --- | --- |
| 単体 | 判定ロジック（公開時刻の計算、encounter の統合、party の状態遷移、geofence、moderation のしきい値、カーソル） | なし。時刻と ID は注入する | `make test` |
| 結合 | repo と service（SQLite と Redis を実際に使う） | 一時ファイルの SQLite、Redis | `make test-integration` |
| API | Fiber のハンドラからレスポンスまで | 結合と同じ。認証は JWT を直接発行して通す | `make test-integration` |
| シナリオ | すれ違い・Party・グループの時系列の筋書き（§4） | 結合と同じ ＋ fake clock | `make test-integration` |
| 外部契約 | NanoGPT の実 API、Jetstream の実データ | ネットワークと API 鍵 | `make test-external`（手動） |
| 実機 | BLE・FGS・電池 | Android 端末 2 台以上 | 手動（§6） |

## 2. テストしやすくするための実装規則

- **時刻**: `time.Now()` を直接呼ばない。`clock.Clock` interface（`Now()`、`After()`、`NewTicker()`）を service とワーカーに注入する。テストでは手で進める `clock.Fake` を使う。`time.Now(` が `internal/clock` 以外に現れないことを `make lint` で確認する
- **ID と乱数**: UUIDv7、TID、beacon token、招待コードの生成は `idgen.Generator` interface を通す。テストでは決まった列を返す実装を使う
- **ワーカーの tick**: `party.Engine` などは `Tick(ctx)` を公開し、テストでは ticker を使わず直接呼ぶ
- **外部 API**: PDS（`atproto.Writer`）、OAuth、Jetstream、`moderation.Moderator` / `PostAssessor`、FCM はすべて interface 越しに使い、テストでは fake に差し替える

## 3. 結合テストの環境

- SQLite: テストごとに `t.TempDir()` に DB を作り、`schema.sql` を流す（`seed.sql` は流さない。必要なデータはテストの中で作る）
- Redis: `CHAIN_TEST_REDIS_URL`（既定 `redis://localhost:6379/15`）。テストの前に `FLUSHDB` する。接続できなければ結合テストを skip する（単体テストだけは Redis なしで動く）
- GEO 系のコマンド（`GEOSEARCH`）を使うため、Redis のエミュレータ（miniredis など）ではなく本物の Redis を使う
- パッケージをまたいで同じ Redis DB を使うので、結合テストは `go test -p 1` で直列に実行する

## 4. シナリオテスト

時系列の筋書きを表の形で書き、fake clock を進めながら service を呼んで結果を確かめる。
少なくとも次の筋書きを用意する。

### 4.1 すれ違い（06）

| # | 筋書き | 期待 |
| --- | --- | --- |
| E1 | A が B を観測、5 分後に B が A を観測（双方 immediate） | 1 件成立し、すぐ revealed |
| E2 | A が B を観測、15 分後に B が A を観測 | 成立しない（`MatchWindow` 10 分を超える） |
| E3 | A が B だけを観測し続ける | 成立しない |
| E4 | E1 の 20 分後に再び双方向の観測 | 同じ encounter が延長される（件数は 1 のまま） |
| E5 | E1 の 40 分後に再び双方向の観測 | 2 件目になる（`MergeGap` 30 分を超える） |
| E6 | A は delayed 1 時間、B は delayed 1 日 | `reveal_at = last_seen + 1 日` |
| E7 | A が private | hidden。一覧に出ず、成立から 1 時間後に summary の回数にだけ入る |
| E8 | pending の間に A が private に変える | hidden になる |
| E9 | pending の間に A が immediate に緩める | 既存の encounter の `reveal_at` は変わらない |
| E10 | 窓の外の時刻（±120 秒を超える）で token を報告 | dropped |
| E11 | B のブロック後に双方向の観測 | 成立しない |
| E12 | B の観測が 20 時間遅れて届く | 時刻どおりに A の観測と照合して成立する |

### 4.2 Party（08）

| # | 筋書き | 期待 |
| --- | --- | --- |
| P1 | フレンド A・B が 30 秒近接 | 仮 party |
| P2 | P1 から 5 分近接が続く | 本 party |
| P3 | 仮 party の途中で 70 秒途切れる | 破棄される |
| P4 | 本 party で B が 10 分不在 | B が離脱し、在席が 2 人未満なので終了・archived |
| P5 | 本 party に、B のフレンドで A のフレンドではない C が近接 | C が同じ party に入る |
| P6 | 仮 party 同士が近接 | 古い方に統合される |
| P7 | 本 party 同士が近接 | 統合されない |
| P8 | locked の本 party で全員が 1 日不在 | 終了しない。72 時間で終了する |
| P9 | `party_auto = 0` のユーザー | どの party にも入らない |
| P10 | 仮 party のうちに投稿しようとする | `403` |
| P11 | 本 party から C が明示的に退出し、C のフレンド D と近接 | D は元の party に入らない。C・D の party ができる |
| P12 | C が 10 分不在で離脱した後、D と近接 | 同上 |
| P13 | 不在で離脱した C が元のメンバーのもとに戻る | 同じ party に再合流する |
| P14 | 明示的に退出した C が元のメンバーのもとに戻る | 再合流しない |
| P15 | 仮 party が統合されずに破棄される | メンバーに `party.updated`（`dissolved`） |
| P16 | 本 party が終了する | 離脱済みを含むメンバーに `party.updated`（`ended`） |
| P17 | 離脱済みのメンバーがいる状態で `party:of` を組み直す | 離脱済みには `party:of` を作らない |
| P18 | 不在で離脱した C が別の本 party に入った後、元のメンバーのもとに戻る | 元の party に二重に所属しない |
| P19 | 明示的に退出した C が元のメンバーの近くに居続ける | どの party にも入らない |

### 4.3 グループ（09）

| # | 筋書き | 期待 |
| --- | --- | --- |
| G1 | geofence の外から参加 | `403 not_in_geofence` |
| G2 | 位置が 3 分前のものしかない状態で参加 | `403 location_stale` |
| G3 | 非 persistent のグループで、参加後 15 分位置が更新されない | 閲覧も `403 location_stale` |
| G4 | persistent のグループで、エリア外から閲覧 | 読める |
| G5 | schedule の時間帯の外で nearby 一覧を取る | 出てこない |
| G6 | 既存とほぼ同じ円・同じ時間帯で作成 | `409`。`force` で作成できる |
| G7 | closed で quorum 2、メンバー 1 人 | 招待者の承認だけで offered になる（`min(quorum, メンバー数)`） |
| G8 | エリアに入る、出る、24 時間以内にまた入る | 入場通知は 1 回だけ |

## 5. 外部との契約テスト

| 対象 | 方法 |
| --- | --- |
| BLE の payload | `testdata/beacon/payload.json` に token と、期待するバイト列（16 進）の組を置く。Go（backend の解析）と Kotlin（native の組み立て）の両方のテストが同じファイルを読む |
| NanoGPT の応答の解析 | 実際の応答を `testdata/nanogpt/*.json` に保存し、しきい値の計算をテストする |
| NanoGPT の実 API | `-tags=external` のときだけ、`CHAIN_NANOGPT_KEY` で実際に 1 件判定する |
| Jev の日本語の精度（README Q10） | `testdata/moderation/ja.jsonl`（本文と、期待する label の組）を `cmd/modeval` で一括判定し、しきい値ごとの適合率と再現率を出す。しきい値を変える根拠にする |
| Jetstream | 実際のイベントの JSON を `testdata/jetstream/*.json` に保存し、consumer に流す |
| lexicon | `backend/lexicons/` の各 record について、正しい例と誤った例で検証のテストをする |

## 6. 実機テスト

`specification/android` §11 の V1〜V5 を確かめる。結果は `docs/research/poc-ble.md` に残す。

| 項目 | 手順 | 記録すること |
| --- | --- | --- |
| 発見までの時間 | 2 台を 0.5 m / 3 m / 10 m に置き、片方で start した時刻から sighting が送られるまで | 時間の中央値と最大値 |
| すれ違い | 2 人が端末を持って歩いてすれ違う（各 10 回） | 成立した回数 |
| 画面オフ | 両方の画面を切って 30 分放置 | sighting が途切れないか |
| 電池 | 画面オフで 1 時間、すれ違いモードのオンとオフを比べる | 電池残量の差 |
| 端末差 | 手元にある別メーカーの端末で同じことをする | メーカー、機種、Android の版 |

## 7. 目安

- 判定ロジック（§1 の単体の対象）は、分岐をすべて通すことを目標にする
- ハンドラは、正常系 1 件と、主な異常系（権限なし、検証エラー、存在しない）をテストする
- CI（GitHub Actions）は、`go vet`、`make lint`、`make test`、Redis を service container にした `make test-integration` を回す。cgo（go-sqlite3）が要るので、`CGO_ENABLED=1` のランナーを使う
