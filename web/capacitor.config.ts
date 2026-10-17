import type { CapacitorConfig } from '@capacitor/cli';

// `CAP_LIVE_RELOAD_URL=http://<host>:3000 pnpm android:sync` のように指定すると
// WebView が同梱の out/ ではなく dev server を読む。未指定時は通常の静的アセット。
const liveReloadUrl = process.env.CAP_LIVE_RELOAD_URL;

const config: CapacitorConfig = {
  appId: 'jp.i32.chain',
  appName: 'chain',
  webDir: 'out',
  ...(liveReloadUrl
    ? {
        server: {
          url: liveReloadUrl,
          cleartext: liveReloadUrl.startsWith('http://'),
        },
      }
    : {}),
};

export default config;
