<!-- BEGIN:nextjs-agent-rules -->

## This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

## Device testing

実機・エミュレータは `scripts/` の CLI 経由で操作する。詳細は `README.md`。

```bash
pnpm emulator start --headless   # 端末が無い場合
pnpm android:smoke               # 起動 + 画面確認 + スクショ
pnpm device tap-text "友達"      # テキストでタップ
pnpm device wait-text "..." 15   # 表示待ち
pnpm device screenshot path.png
pnpm device cdp 9222             # WebView の DOM を CDP で操作
pnpm test:e2e                    # ブラウザ E2E
pnpm test:e2e:android            # WebView E2E (要 debug APK)
```

- `adb` は `~/Library/Android/sdk/platform-tools/adb`。PATH 不要。
- 端末が複数ある場合は `-s <serial>` か `ANDROID_SERIAL` を付ける。
- 変更後は `pnpm lint` / `pnpm exec tsc --noEmit` / `pnpm build` を通す。

