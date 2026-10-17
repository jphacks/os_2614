# CHAIN

近くにいる人と気軽につながる SNS のフロントエンドです。

- **web**: Next.js 16 (App Router, static export) + React 19 + TypeScript
- **Android**: Capacitor 8 (WebView に `out/` を同梱)
- **backend**: Go (Fiber) のスタブ。現在の画面は `src/data/mock.ts` のモックで動くため、起動しなくても確認できます。

## 必要なもの

- Node.js 24 以降 / pnpm 12 (`packageManager` で固定)
- Android SDK (`~/Library/Android/sdk`)、`adb`、Android Emulator
- JDK 17 以降 (この Mac では Homebrew の OpenJDK 25 で動作確認済み)

```bash
cd web
pnpm install
```

Android SDK の場所が標準と違う場合:

```bash
export ANDROID_HOME=/path/to/android/sdk
```

スクリプトは `ANDROID_HOME` → `ANDROID_SDK_ROOT` → `~/Library/Android/sdk` の順で探すので、PATH を通す必要はありません。

## 1. Mac のブラウザで動かす

```bash
pnpm dev          # http://localhost:3000
pnpm dev:lan      # 同じ Wi-Fi の端末から http://<MacのIP>:3000
```

`pnpm dev:lan` は `0.0.0.0` で待ち受けます。macOS のファイアウォールを有効にしている場合は、Node の受信接続を許可してください。

## ログイン

ログインは backend の `/auth/login` から Bluesky の認可画面へ進み、戻ってきた one-time code を `/auth/callback/` で交換します (specification/backend/03 §2)。

- `NEXT_PUBLIC_CHAIN_API_BASE`: backend の URL。既定は `https://chain.i32.jp`。static export なのでビルド時に埋め込まれます
- ブラウザで試すときは、backend の `CHAIN_APP_REDIRECT` を `http://localhost:3000/auth/callback/` にしてください

  ```bash
  cd backend
  CHAIN_APP_REDIRECT=http://localhost:3000/auth/callback/ go run .
  ```

- Android は `chain://auth/callback` を受け取ります (`android/app/src/main/AndroidManifest.xml`)

## 2. Mac の Android エミュレータで動かす

AVD はこの Mac に作成済みです (`chain`, Pixel 7 / API 36 / arm64)。

```bash
pnpm emulator start              # ウィンドウあり
pnpm emulator start --headless   # 画面なし (AI agent / CI 向け)
pnpm emulator status
pnpm emulator stop
```

アプリを入れて起動する:

```bash
pnpm android:install   # next build -> cap sync -> assembleDebug -> adb install
pnpm device launch     # jp.i32.chain/.MainActivity を起動
pnpm device screenshot # artifacts/ に PNG を保存
```

開発中はライブリロードが便利です。`adb reverse` を使うため、USB でも Wi-Fi でも同じコマンドで動きます。

```bash
pnpm android:live      # dev server + cap run --live-reload (Ctrl+C で終了)
```

> `pnpm android:live` は `next dev` と同じく foreground で動き続けます。AI agent から使うときはバックグラウンドで起動してください。

## 3. 自分の Android 端末で動かす

### USB

1. 端末の「開発者向けオプション」→「USB デバッグ」を有効にする
2. USB で Mac につなぎ、端末に出る「USB デバッグを許可しますか?」を許可する
3. 認識を確認する

```bash
pnpm device devices
pnpm android:install
pnpm android:live
```

### ワイヤレスデバッグ (Android 11+)

端末の「ワイヤレスデバッグ」→「ペア設定コードで端末をペア設定」に表示される IP:ポートと 6 桁コードを使います。

```bash
pnpm device pair 192.168.1.23:37123 123456
pnpm device connect 192.168.1.23:5555
pnpm device devices
```

接続を切る:

```bash
pnpm device disconnect 192.168.1.23:5555
```

端末が複数つながっている場合は `ANDROID_SERIAL` か `-s` で対象を指定します。

```bash
pnpm device -s emulator-5554 screenshot
ANDROID_SERIAL=192.168.1.23:5555 pnpm android:install
```

## 4. リリース用の静的ビルド

```bash
pnpm android:run   # next build -> cap sync -> cap run (ライブリロードなし)
```

`pnpm build` 単体では `out/` を作るだけです。Android に入れるときは `cap sync` が必要なので、通常は `android:install` か `android:run` を使ってください。

## 5. AI agent によるテスト

### 最短スモークテスト

```bash
pnpm android:smoke           # 端末が無ければヘッドレスエミュレータを自動起動
pnpm android:smoke --build   # ビルドからやり直す
```

実行内容:

1. 接続中の端末を確認 (無ければ `chain` AVD を headless 起動)
2. アプリが未インストールなら install
3. アニメーションを無効化
4. 起動して「ちかく」が表示されるまで待つ (最大 60 秒)
5. `artifacts/smoke-<serial>.png` と `.xml` を保存
6. WebView の CDP ポートを案内

失敗時は非ゼロで終了します。

### 画面操作のプリミティブ

`pnpm device` は実機・エミュレータの両方で同じように動きます。`uiautomator` の UI ツリーは WebView 内のテキストも取れるため、日本語ラベルでタップできます。

```bash
pnpm device ui                                   # UI ツリーを保存してテキスト一覧を表示
pnpm device wait-text "友達" 30
pnpm device tap-text "友達"
pnpm device tap 540 1200
pnpm device swipe 540 1800 540 600 300
pnpm device type "hello"                         # ASCII のみ
pnpm device back
pnpm device screenshot artifacts/friends.png
pnpm device logs 200
```

- タップ後の確認は `wait-text` → `screenshot` の順が安定します。
- 日本語入力は `adb shell input text` が非対応です。WebView なら CDP (下記) で `page.type()` を使ってください。
- `pnpm device cdp` は起動中のアプリの WebView DevTools を `http://127.0.0.1:9222` に転送します。debug APK が必要です。

### DOM レベルの WebView テスト

```bash
pnpm device cdp 9222
pnpm test:e2e:android
```

`e2e/android-webview.mjs` が Puppeteer で WebView に接続し、画面テキスト・タブ遷移を検証して `artifacts/android-webview.png` を保存します。

### ブラウザの E2E

```bash
pnpm test:e2e
```

Playwright で desktop / Pixel 7 の 2 ビューポートを検証します。システムの Google Chrome を使う設定です。レポートは `playwright-report/`、失敗時の trace は `test-results/` に出ます。

### agent に渡すときの例

```text
pnpm android:smoke --build で起動確認。
pnpm device tap-text "友達" で友達タブへ移動し、
pnpm device wait-text "位置はエリア名だけ共有しています" で確認。
pnpm device screenshot artifacts/verify.png を撮って報告。
```

成果物 (`artifacts/`, `test-results/`, `playwright-report/`) は `.gitignore` 済みです。

## トラブルシューティング

| 症状 | 対処 |
| --- | --- |
| `adb: command not found` | スクリプトは SDK 内の adb を直接使うので不要。手で叩くなら `export PATH="$HOME/Library/Android/sdk/platform-tools:$PATH"` |
| `unauthorized` | 端末側で USB デバッグの許可ダイアログを承認する。出ない場合はケーブルを抜き差し |
| エミュレータが起動しない | `pnpm emulator status` と `artifacts/emulator.log` を確認。`--cold` でコールドブート |
| 画面が真っ白 / CDP が繋がらない | debug APK か確認 (`pnpm android:install`)。`pnpm device launch` 後に `pnpm device cdp` |
| `wait-text` がタイムアウトする | `pnpm device ui` で実際のテキストを確認。WebView の読み込み待ちなら数秒待って再実行 |
| `pnpm android:live` が戻らない | 仕様です。`next dev` と同じ foreground プロセスです。終了は Ctrl+C |
| port 3000 が使用中 | `pnpm android:live` は既存の dev server を再利用します。別ポートなら `CHAIN_DEV_PORT=3001 pnpm android:live` |
| `Failed to install` | 端末の空き容量と `adb devices` の状態を確認。`pnpm device clear` でアプリデータ削除 |

## 補足

- iOS は未設定です (`ios/` がありません)。必要なら Xcode を入れて `npx cap add ios` から追加してください。
- バックエンドを動かす場合:

```bash
cd backend
go run .
# http://localhost:8080/api
```

- 端末の BLE / 位置情報プラグインは `src/device/proximity/` 側で実装しています。
