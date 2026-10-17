import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
} from "@playwright/test";

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";
const TARGET_DID = "did:plc:tom";

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

/** GET /users/{did} (13 §1)。統計 (counts) は返らない。 */
const PROFILE = {
  did: TARGET_DID,
  handle: "tom.bsky.social",
  displayName: "トム",
  description: "休日は公園を散歩しています。",
  status: "散歩中",
};

/** GET /me。対象 DID との一致判定にだけ使う。 */
const ME = {
  did: "did:plc:testuser",
  handle: "test.bsky.social",
  displayName: "てすとたろう",
};

/** GET /feed?source=user:{did} (17 §3.2)。 */
const FEED = {
  items: [
    {
      kind: "public",
      uri: "at://did:plc:tom/jp.i32.chain.feed.post/1",
      author: { did: TARGET_DID, handle: "tom.bsky.social" },
      text: "公園でひと休み",
      createdAt: "2026-10-17T00:00:00.000Z",
    },
  ],
};

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

function fakeJwt(claims: Record<string, unknown>): string {
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify(claims));
  return `${header}.${payload}.signature`;
}

function makeStoredSession(): StoredSession {
  const did = "did:plc:testuser";
  const now = Math.floor(Date.now() / 1000);
  return {
    did,
    accessToken: fakeJwt({ sub: did, iat: now, exp: now + 900 }),
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

/** DID を含むパスはパーセントエンコードされるので、比較用に戻す。 */
function apiPath(url: string): string {
  return decodeURIComponent(new URL(url).pathname);
}

type MockOptions = {
  /** /users/{did} の応答。200 以外なら同じステータスでエラーを返す。 */
  profileStatus?: number;
  /** /me の応答。自分自身を開くテストで上書きする。 */
  me?: unknown;
  /**
   * POST /friends/requests (07 §2.2) の応答。既定は 201 と {}。
   * 400 以上のときの本文は既存と同じ { error: { code, message } } 形式。
   */
  friendRequest?: { status: number; body?: unknown };
};

/**
 * /users/{did}、/feed、/me、POST /friends/requests をモックする。
 * feedSources には GET /feed の source クエリを、friendRequests には
 * POST /friends/requests の JSON body を到着順に積む。
 */
async function mockProfileApi(
  page: Page,
  opts: MockOptions = {},
): Promise<{ feedSources: string[]; friendRequests: unknown[] }> {
  const feedSources: string[] = [];
  const friendRequests: unknown[] = [];
  await mockApi(page, async (route, request) => {
    const url = new URL(request.url());
    const path = apiPath(request.url());

    if (path === "/api/v1/friends/requests" && request.method() === "POST") {
      friendRequests.push(request.postDataJSON());
      const status = opts.friendRequest?.status ?? 201;
      const body =
        opts.friendRequest?.body ??
        (status >= 400
          ? { error: { code: "bad_request", message: "申請を送れませんでした" } }
          : {});
      await fulfillJson(route, status, body);
      return;
    }
    if (path === "/api/v1/me") {
      await fulfillJson(route, 200, opts.me ?? ME);
      return;
    }
    if (path === `/api/v1/users/${TARGET_DID}`) {
      const status = opts.profileStatus ?? 200;
      if (status === 200) {
        await fulfillJson(route, 200, PROFILE);
      } else {
        await fulfillJson(route, status, {
          error: { code: "internal", message: "boom" },
        });
      }
      return;
    }
    if (path === "/api/v1/feed") {
      feedSources.push(url.searchParams.get("source") ?? "");
      await fulfillJson(route, 200, FEED);
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });
  return { feedSources, friendRequests };
}

/** プロフィールの主要部分 (タブバーを含まない <main>)。 */
function profileMain(page: Page) {
  return page.locator("main");
}

test.describe("user profile", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("renders the other user's name, handle, bio and status", async ({
    page,
  }) => {
    await mockProfileApi(page);

    await page.goto(`/users/?did=${TARGET_DID}`);

    await expect(
      page.getByRole("heading", { name: PROFILE.displayName }),
    ).toBeVisible();
    await expect(page.getByText(`@${PROFILE.handle}`)).toBeVisible();
    await expect(page.getByText(PROFILE.description)).toBeVisible();
    await expect(page.getByText(PROFILE.status)).toBeVisible();
    // GET /users/{did} は統計を返さないため、すれ違いの数字は出さない。
    await expect(page.getByText("すれ違い", { exact: true })).toHaveCount(0);
    await expect(page.getByText("公園でひと休み")).toBeVisible();
  });

  test("loads public posts from /feed?source=user:{did}", async ({ page }) => {
    const { feedSources } = await mockProfileApi(page);

    await page.goto(`/users/?did=${TARGET_DID}`);

    const section = page.locator("section", { hasText: "公開投稿" });
    await expect(section.getByText("公園でひと休み")).toBeVisible();
    await expect
      .poll(() => feedSources)
      .toContain(`user:${TARGET_DID}`);
  });

  test("shows the unavailable state when GET /users/{did} is 404", async ({
    page,
  }) => {
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto(`/users/?did=${TARGET_DID}`);

    await expect(page.getByText("プロフィールは準備中です")).toBeVisible();
  });

  test("shows the error state and retries GET /users/{did} after a 500", async ({
    page,
  }) => {
    let fail = true;
    let profileCalls = 0;
    await mockApi(page, async (route, request) => {
      const path = apiPath(request.url());
      if (path === `/api/v1/users/${TARGET_DID}`) {
        profileCalls += 1;
        if (fail) {
          await fulfillJson(route, 500, {
            error: { code: "internal", message: "boom" },
          });
          return;
        }
        await fulfillJson(route, 200, PROFILE);
        return;
      }
      if (path === "/api/v1/me" || path === "/api/v1/feed") {
        await fulfillJson(
          route,
          200,
          path === "/api/v1/me" ? ME : { items: [] },
        );
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto(`/users/?did=${TARGET_DID}`);

    await expect(
      page.getByText("プロフィールを読み込めませんでした"),
    ).toBeVisible();
    fail = false;
    await page.getByRole("button", { name: "再読み込み" }).click();

    await expect(
      page.getByRole("heading", { name: PROFILE.displayName }),
    ).toBeVisible();
    expect(profileCalls).toBeGreaterThan(1);
  });

  test("links to the own profile when the target DID is yourself", async ({
    page,
  }) => {
    await mockProfileApi(page, {
      me: { did: TARGET_DID, handle: "tom.bsky.social", displayName: "トム" },
    });

    await page.goto(`/users/?did=${TARGET_DID}`);

    const link = page.getByRole("link", { name: "自分のプロフィール" });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute("href", /^\/profile\/?$/);
  });

  test("sends the encounter as the via ref and switches to 申請済み", async ({
    page,
  }) => {
    const { friendRequests } = await mockProfileApi(page);

    await page.goto(
      `/users/?did=${TARGET_DID}&encounter=enc-1&relation=none`,
    );

    const button = page.getByRole("button", { name: "友達申請" });
    await expect(button).toBeVisible();
    await button.click();

    await expect.poll(() => friendRequests.length).toBe(1);
    expect(friendRequests[0]).toEqual({
      did: TARGET_DID,
      via: "encounter",
      viaRef: "enc-1",
    });

    await expect(page.getByRole("button", { name: "申請済み" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "友達申請" })).toHaveCount(0);
  });

  test("shows the requested relation as a disabled 申請済み", async ({ page }) => {
    await mockProfileApi(page);

    await page.goto(
      `/users/?did=${TARGET_DID}&encounter=enc-1&relation=requested`,
    );

    await expect(page.getByRole("button", { name: "申請済み" })).toBeDisabled();
  });

  test("shows the incoming relation as a link to the requests screen", async ({
    page,
  }) => {
    await mockProfileApi(page);

    await page.goto(
      `/users/?did=${TARGET_DID}&encounter=enc-1&relation=incoming`,
    );

    const link = profileMain(page).getByRole("link", {
      name: /この人から友達申請が届いています/,
    });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute("href", "/friends/requests/");
  });

  test("shows a 友達 tag for the friend relation", async ({ page }) => {
    await mockProfileApi(page);

    await page.goto(
      `/users/?did=${TARGET_DID}&encounter=enc-1&relation=friend`,
    );

    await expect(
      profileMain(page).getByText("友達", { exact: true }),
    ).toBeVisible();
  });

  test("hides the request UI when the encounter query is missing", async ({
    page,
  }) => {
    await mockProfileApi(page);

    await page.goto(`/users/?did=${TARGET_DID}&relation=none`);

    await expect(page.getByText(PROFILE.description)).toBeVisible();
    await expect(page.getByRole("button", { name: "友達申請" })).toHaveCount(0);
  });

  test("shows 友達申請はまだ使えません when POST is 501", async ({ page }) => {
    await mockProfileApi(page, { friendRequest: { status: 501 } });

    await page.goto(
      `/users/?did=${TARGET_DID}&encounter=enc-1&relation=none`,
    );
    await page.getByRole("button", { name: "友達申請" }).click();

    await expect(page.getByText("友達申請はまだ使えません")).toBeVisible();
  });

  test("shows the error message when POST fails with 400", async ({ page }) => {
    await mockProfileApi(page, {
      friendRequest: {
        status: 400,
        body: { error: { code: "invalid_request", message: "bad" } },
      },
    });

    await page.goto(
      `/users/?did=${TARGET_DID}&encounter=enc-1&relation=none`,
    );
    await page.getByRole("button", { name: "友達申請" }).click();

    await expect(page.getByText("bad")).toBeVisible();
  });
});
