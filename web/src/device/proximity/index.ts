// ChainProximity プラグインのエントリ。native (Android) は Capacitor ブリッジ経由、
// ブラウザ (pnpm dev) は web.ts のモックが実装する。
//
// このファイルはサーバ側 (static export の prerender) でも import されうるので
// window などのブラウザ専用 API には触らない。web.ts は動的 import なので、
// ブラウザでの実行時に初めて読み込まれる。

import { registerPlugin } from "@capacitor/core";
import type { ChainProximityPlugin } from "./definitions";

// Capacitor の loadPluginImplementation は「web 実装のファクトリ」を呼び出しごとに
// 実行しうる。useLocation のように addListener と startLocationUpdates を続けて呼ぶと
// 初回だけファクトリが並行に走り、リスナー登録と emit が別インスタンスになって
// location イベントが hook に届かない (StrictMode の二重マウントで顕著)。
// ファクトリが返す Promise を 1 度だけ作って共有し、常に同一インスタンスを使う。
type WebImplementation = import("./web").ChainProximityWeb;

let webImplementation: Promise<WebImplementation> | undefined;
function loadWebImplementation(): Promise<WebImplementation> {
  webImplementation ??= import("./web").then((m) => new m.ChainProximityWeb());
  return webImplementation;
}

export const ChainProximity = registerPlugin<ChainProximityPlugin>("ChainProximity", {
  web: loadWebImplementation,
});

export * from "./definitions";
