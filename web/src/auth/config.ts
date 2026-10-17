// backend の場所。static export なのでビルド時に埋め込まれる。
// ブラウザで試すときは backend の CHAIN_APP_REDIRECT を http://localhost:3000/auth/callback/ にする。
export const API_BASE = (process.env.NEXT_PUBLIC_CHAIN_API_BASE ?? "https://chain.i32.jp").replace(
  /\/+$/,
  "",
);

/** access token の期限までこれを切ったら、使う前に refresh する (specification/android §3)。 */
export const REFRESH_MARGIN_MS = 60_000;

/** backend が端末アプリへ戻す deep link (specification/backend/03 §2)。 */
export const APP_CALLBACK_URLS = ["chain://auth/callback", `${API_BASE}/app/auth/callback`];

/** deep link / ブラウザのリダイレクトを受けて、one-time code を交換する画面。 */
export const CALLBACK_PATH = "/auth/callback/";
export const WELCOME_PATH = "/welcome/";
