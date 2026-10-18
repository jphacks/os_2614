import { expect, test, type Page, type Route } from "@playwright/test";

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";

const CORS_HEADERS: Record<string, string> = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,PATCH,PUT,DELETE,OPTIONS",
  "access-control-allow-headers": "authorization,content-type",
  "access-control-max-age": "600",
};

/** GET /me の meResponse (backend/internal/controller/me.go)。O1 で pdsHost が付く。 */
const ME = {
  did: "did:plc:testuser",
  handle: "test.bsky.social",
  displayName: "てすとたろう",
  description: "近所のカフェでもくもくしています。",
  pdsHost: "pds.example",
  settings: {
    encounterMode: "delayed",
    encounterDelaySec: 3600,
    friendBeaconShare: true,
    partyAuto: false,
    locationPaused: false,
    warnDisplay: "blur",
    notify: {},
  },
};

/** GET /me/data-map (backend/internal/controller/me.go dataMapResponse)。 */
const DATA_MAP = {
  pdsUrl: "https://pds.example",
  pdsHost: "pds.example",
  scopes: ["atproto", "repo:app.bsky.feed.post?action=create"],
  counts: { publicPosts: 3, spacePosts: 5, photos: 2, encounters: 7, friends: 4 },
  rows: [
    { id: "public_posts", data: "公開投稿・プロフィール", storage: "pds", place: "あなたの PDS（https://pds.example）", visibleTo: "誰でも", retention: "消すまで", export: "PDS のエクスポート", countKeys: ["publicPosts"] },
    { id: "space_posts", data: "グループ / Party の投稿・写真", storage: "chain", place: "CHAIN のサーバー", visibleTo: "その Space のメンバーと、CHAIN の運営者", retention: "archive 後も残る", export: "エクスポート機能（準備中）", countKeys: ["spacePosts", "photos"] },
    { id: "friends_follow", data: "フレンド（フォロー）", storage: "pds", place: "あなたの PDS（app.bsky.graph.follow）", visibleTo: "誰でも", retention: "消すまで", export: "PDS のエクスポート", countKeys: ["friends"] },
    { id: "encounters", data: "すれ違い・会った裏付け", storage: "chain", place: "CHAIN のサーバー", visibleTo: "本人と相手、運営者", retention: "13 §5 の保存期間", export: "エクスポート機能（準備中）", countKeys: ["encounters"] },
    { id: "location", data: "位置", storage: "chain", place: "CHAIN のサーバー（一時的なもの。Redis）", visibleTo: "位置共有の相手、運営者", retention: "数分〜数時間で自動で消える", export: "持ち出せない（保存していない）", countKeys: [] },
  ],
};

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

function fakeJwt(claims: Record<string, unknown>): string {
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  return `${header}.${base64url(JSON.stringify(claims))}.signature`;
}

async function useStoredSession(page: Page): Promise<void> {
  const now = Math.floor(Date.now() / 1000);
  const session = {
    did: "did:plc:testuser",
    accessToken: fakeJwt({ sub: "did:plc:testuser", iat: now, exp: now + 900 }),
    refreshToken: "refresh-token-0",
    accessExpiresAtMs: Date.now() + 60 * 60 * 1000,
  };
  await page.addInitScript(
    ({ key, value }) => window.localStorage.setItem(key, value),
    { key: SESSION_KEY, value: JSON.stringify(session) },
  );
}

async function fulfillJson(route: Route, status: number, body: unknown): Promise<void> {
  await route.fulfill({
    status,
    headers: { ...CORS_HEADERS, "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

async function mockApi(page: Page, dataMap: { status: number; body: unknown } = { status: 200, body: DATA_MAP }): Promise<void> {
  await page.route(`${API_BASE}/**`, async (route) => {
    const request = route.request();
    if (request.method() === "OPTIONS") {
      await route.fulfill({ status: 204, headers: CORS_HEADERS });
      return;
    }
    const path = new URL(request.url()).pathname;
    if (path === "/api/v1/me") {
      await fulfillJson(route, 200, ME);
      return;
    }
    if (path === "/api/v1/me/data-map") {
      await fulfillJson(route, dataMap.status, dataMap.body);
      return;
    }
    await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
  });
}

test.use({ trace: "off" });
test.setTimeout(60_000);

test.describe("data placement (O1 / O4)", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page);
  });

  test("profile shows the PDS host and links to the data map", async ({ page }) => {
    await mockApi(page);
    await page.goto("/profile/");

    await expect(page.getByText("あなたのデータは")).toContainText("pds.example");
    await page.getByRole("link", { name: "データの置き場所" }).click();
    await expect(page).toHaveURL(/\/settings\/data\/$/);
    await expect(page.getByTestId("data-row-public_posts")).toBeVisible();
  });

  test("settings links to the data map", async ({ page }) => {
    await mockApi(page);
    await page.goto("/settings/");
    await page.getByRole("link", { name: /データの置き場所/ }).click();
    await expect(page).toHaveURL(/\/settings\/data\/$/);
  });

  test("renders the table, counts, scopes and the revoke guide", async ({ page }) => {
    await mockApi(page);
    await page.goto("/settings/data/");

    const posts = page.getByTestId("data-row-public_posts");
    await expect(posts).toContainText("あなたの PDS（https://pds.example）");
    await expect(posts).toContainText("公開投稿 3件");

    // フレンドは PDS の follow と、CHAIN の側のすれ違いの 2 行に分かれる。
    await expect(page.getByTestId("data-row-friends_follow")).toContainText("app.bsky.graph.follow");
    await expect(page.getByTestId("data-row-friends_follow")).toContainText("フレンド 4件");
    await expect(page.getByTestId("data-row-encounters")).toContainText("すれ違い 7件");
    await expect(page.getByTestId("data-row-space_posts")).toContainText("投稿 5件・写真 2件");

    const scopes = page.getByTestId("scopes");
    await expect(scopes).toContainText("atproto");
    await expect(scopes).toContainText("repo:app.bsky.feed.post?action=create");

    await expect(page.getByRole("heading", { name: "CHAIN へのアクセスを取り消す" })).toBeVisible();
    await expect(page.getByRole("link", { name: /Bluesky の設定を開く/ })).toHaveAttribute(
      "href",
      "https://bsky.app/settings",
    );
    await expect(page.getByRole("button", { name: "ログアウト" })).toBeVisible();
  });

  test("scopes null (expired session) shows a re-login note", async ({ page }) => {
    await mockApi(page, { status: 200, body: { ...DATA_MAP, scopes: null } });
    await page.goto("/settings/data/");

    await expect(page.getByTestId("scopes-unknown")).toBeVisible();
    await expect(page.getByTestId("scopes")).toHaveCount(0);
    await expect(page.getByTestId("data-row-public_posts")).toBeVisible();
  });

  test("unavailable endpoint shows the preparing state", async ({ page }) => {
    await mockApi(page, { status: 404, body: { error: { code: "not_found", message: "not found" } } });
    await page.goto("/settings/data/");
    await expect(page.getByText("データの置き場所は準備中です")).toBeVisible();
  });
});
