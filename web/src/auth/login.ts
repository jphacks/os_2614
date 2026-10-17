// ログインの開始と、backend からの戻り (deep link) の受け取り。流れは specification/backend/03 §2。
//
// - Android: Custom Tabs (Browser プラグイン) で /auth/login を開く。backend は認可後に
//   chain://auth/callback?code=... (本番は App Links) へ戻すので、appUrlOpen で受けて Custom Tabs を閉じる。
// - ブラウザ: 同じタブで /auth/login へ遷移する。backend の CHAIN_APP_REDIRECT を CALLBACK_PATH に向けておく。
// どちらも最後は CALLBACK_PATH の画面が code を交換する。

import { App } from "@capacitor/app";
import { Browser } from "@capacitor/browser";
import { Capacitor } from "@capacitor/core";
import { API_BASE, APP_CALLBACK_URLS } from "./config";

export async function startLogin(handle?: string): Promise<void> {
  const url = new URL(`${API_BASE}/auth/login`);
  const trimmed = handle?.trim().replace(/^@/, "");
  if (trimmed) {
    url.searchParams.set("handle", trimmed);
  }
  if (Capacitor.isNativePlatform()) {
    await Browser.open({ url: url.toString() });
  } else {
    window.location.assign(url.toString());
  }
}

/** deep link が backend からの戻りなら、その query (code または error) を返す。 */
export function parseCallbackUrl(raw: string): URLSearchParams | null {
  const base = APP_CALLBACK_URLS.find(
    (prefix) => raw === prefix || raw.startsWith(`${prefix}?`) || raw.startsWith(`${prefix}/?`),
  );
  if (!base) {
    return null;
  }
  const q = raw.indexOf("?");
  const params = new URLSearchParams(q >= 0 ? raw.slice(q + 1) : "");
  return params.has("code") || params.has("error") ? params : null;
}

/**
 * native でだけ使う。起動中に届いた deep link と、deep link でのコールド起動の両方を拾う。
 * 戻り値で購読を解除する。
 */
export function listenForAuthCallback(onCallback: (params: URLSearchParams) => void): () => void {
  let disposed = false;
  const handle = (url: string | undefined) => {
    const params = url ? parseCallbackUrl(url) : null;
    if (!params || disposed) {
      return;
    }
    // Custom Tabs が前面に残っていることがあるので閉じる。既に閉じていれば reject されるので無視する。
    void Browser.close().catch(() => {});
    onCallback(params);
  };

  const registration = App.addListener("appUrlOpen", (e) => handle(e.url));
  void App.getLaunchUrl()
    .then((launch) => handle(launch?.url))
    .catch(() => {});

  return () => {
    disposed = true;
    void registration.then((h) => h.remove());
  };
}
