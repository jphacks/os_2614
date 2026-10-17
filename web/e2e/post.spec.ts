import { expect, test, type Page, type Request, type Route } from "@playwright/test";

// いいね / リアクションは backend が P2 (specification/backend/README.md:71)。
// 対応エンドポイントが無いので、PostCard / ReplySheet はいいね UI を描画せず、
// ローカル状態で「保存された」ようにも見せない (todo 45)。

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";

const CORS_HEADERS: Record<string, string> = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,PATCH,PUT,DELETE,OPTIONS",
  "access-control-allow-headers": "authorization,content-type",
  "access-control-max-age": "600",
};

type StoredSession = {
  did: string;
  accessToken: string;
  refreshToken: string;
  accessExpiresAtMs: number;
};

type ApiMock = (route: Route, request: Request) => Promise<void> | void;

const GROUP = {
  id: "g1",
  name: "情報工学科 3年",
  description: "",
  visibility: "open",
  memberCount: 8,
  isMember: true,
  state: "active",
  radiusM: 40,
};

const ME = {
  did: "did:plc:testuser",
  handle: "test.bsky.social",
  displayName: "てすとたろう",
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

/** GET /instances/{spaceId}/posts の 1 件 (service.FeedItem、10 §4.2)。 */
const POST = {
  kind: "post",
  uri: "at://did:plc:testuser/jp.i32.chain.instance.post/p1",
  spaceId: "g1",
  author: {
    did: "did:plc:author",
    handle: "kenta.bsky.social",
    displayName: "けんた",
  },
  text: "いいねの無い投稿です。",
  format: "plain",
  media: [],
  createdAt: "2026-10-17T02:30:00.000Z",
};

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

function makeStoredSession(): StoredSession {
  const did = "did:plc:testuser";
  const now = Math.floor(Date.now() / 1000);
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify({ sub: did, iat: now, exp: now + 900 }));
  return {
    did,
    accessToken: `${header}.${payload}.signature`,
    refreshToken: "refresh-token-0",
    accessExpiresAtMs: Date.now() + 60 * 60 * 1000,
  };
}

async function useStoredSession(page: Page, session: StoredSession): Promise<void> {
  await page.addInitScript(
    ({ key, value }) => {
      window.localStorage.setItem(key, value);
    },
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

async function mockApi(page: Page, mock: ApiMock): Promise<void> {
  await page.route(`${API_BASE}/**`, async (route) => {
    const request = route.request();
    if (request.method() === "OPTIONS") {
      await route.fulfill({ status: 204, headers: CORS_HEADERS });
      return;
    }
    await mock(route, request);
  });
}

function apiPath(url: string): string {
  return decodeURIComponent(new URL(url).pathname);
}

/**
 * group / 返信画面の共通 mock。投稿は返信ページでも同じ一覧から引けるようにする。
 * いいね系のパスが叩かれたら即座に分かるよう 500 を返す (本来呼ばれない)。
 */
async function mockTimeline(page: Page): Promise<void> {
  await mockApi(page, async (route, request) => {
    const method = request.method();
    const pathname = apiPath(request.url());
    if (method === "GET" && pathname === "/api/v1/groups/g1") {
      await fulfillJson(route, 200, GROUP);
      return;
    }
    if (method === "GET" && pathname === "/api/v1/instances/g1/posts") {
      await fulfillJson(route, 200, { items: [POST] });
      return;
    }
    if (method === "GET" && pathname === "/api/v1/instances/g1/pins") {
      await fulfillJson(route, 200, { items: [] });
      return;
    }
    if (method === "GET" && pathname === "/api/v1/me") {
      await fulfillJson(route, 200, ME);
      return;
    }
    if (/\/likes?$|\/reactions?$/.test(pathname)) {
      await fulfillJson(route, 500, {
        error: { code: "unexpected", message: "reaction API must not be called" },
      });
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });
}

/** 画面から出たリクエストのうち、いいね / リアクションらしきものだけを集める。 */
function collectReactionRequests(page: Page): string[] {
  const seen: string[] = [];
  page.on("request", (request) => {
    if (/\/likes?(\?|$)|\/reactions?(\?|$)/.test(request.url())) {
      seen.push(request.url());
    }
  });
  return seen;
}

test.describe("like UI is absent (backend P2)", () => {
  test("PostCard renders reply only, with no like button or reaction request", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());
    await mockTimeline(page);
    const reactionRequests = collectReactionRequests(page);

    await page.goto("/group/?id=g1");

    await expect(page.getByText(POST.text)).toBeVisible();

    // いいね UI は描画されない (aria-label / aria-pressed の双方で確認)。
    await expect(page.getByRole("button", { name: "いいね" })).toHaveCount(0);
    await expect(page.locator("button[aria-pressed]")).toHaveCount(0);

    // 返信導線は残る。
    const reply = page.getByRole("button", { name: "返信" });
    await expect(reply).toBeVisible();
    await reply.click();

    await expect(page.getByRole("dialog", { name: "返信" })).toBeVisible();
    expect(reactionRequests).toEqual([]);
  });

  test("ReplySheet lists replies without like controls", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockTimeline(page);
    const reactionRequests = collectReactionRequests(page);

    await page.goto(
      "/group/replies/?id=g1&post=at%3A%2F%2Fdid%3Aplc%3Atestuser%2Fjp.i32.chain.instance.post%2Fp1",
    );

    const dialog = page.getByRole("dialog", { name: "返信" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(POST.text)).toBeVisible();

    // 返信行にもいいねは無い (ローカル状態で保存済みに見せない)。
    await expect(dialog.getByRole("button", { name: "いいね" })).toHaveCount(0);
    await expect(dialog.locator("button[aria-pressed]")).toHaveCount(0);
    expect(reactionRequests).toEqual([]);
  });
});
