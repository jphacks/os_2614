// ChainProximity プラグインのエントリ。native (Android) は Capacitor ブリッジ経由、
// ブラウザ (pnpm dev) は web.ts のモックが実装する。
//
// このファイルはサーバ側 (static export の prerender) でも import されうるので
// window などのブラウザ専用 API には触らない。web.ts は動的 import なので、
// ブラウザでの実行時に初めて読み込まれる。

import { registerPlugin } from "@capacitor/core";
import type { ChainProximityPlugin } from "./definitions";

export const ChainProximity = registerPlugin<ChainProximityPlugin>("ChainProximity", {
  web: () => import("./web").then((m) => new m.ChainProximityWeb()),
});

export * from "./definitions";
