import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
} from "@playwright/test";

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";

const CORS_HEADERS: Record<string, string> = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,PATCH,PUT,DELETE,OPTIONS",
  "access-control-allow-headers": "authorization,content-type",
  "access-control-max-age": "600",
};

const ME = {
  did: "did:plc:testuser",
  handle: "testuser.bsky.social",
  displayName: "テストユーザー",
  settings: {
    encounterMode: "delayed",
    encounterDelaySec: 3600,
    friendBeaconShare: true,
    partyAuto: true,
    locationPaused: false,
    warnDisplay: "blur",
    notify: {},
  },
};

/** GET /groups/nearby (09 §5) の NearbyResult。inside は距離なし、near は帯のみ。 */
const NEARBY = {
  inside: [
    {
      id: "g1",
      name: "情報工学科 3年",
      memberCount: 12,
      isMember: false,
      persistent: true,
      radiusM: 120,
    },
  ],
  near: [],
};

const GROUP_G1 = {
  id: "g1",
  name: "情報工学科 3年",
  description: "",
  visibility: "open",
  memberCount: 12,
  isMember: false,
  state: "active",
  radiusM: 120,
  centerLat: 35,
  centerLon: 135,
};

type StoredSession = {
  did: string;
  accessToken: string;
  refreshToken: string;
  accessExpiresAtMs: number;
};

type ApiMock = (route: Route, request: Request) => Promise<void> | void;

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

function fakeJwt(claims: Record<string, unknown>): string {
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify(claims));
  return `${header}.${payload}.signature`;
}

function jwtFor(did: string): string {
  const now = Math.floor(Date.now() / 1000);
  return fakeJwt({ sub: did, iat: now, exp: now + 900 });
}

function makeStoredSession(): StoredSession {
  const did = "did:plc:testuser";
  return {
    did,
    accessToken: jwtFor(did),
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

/** 新しい「ちかく」画面が叩く GET の既定応答。テストごとに上書きする。 */
async function mockDefaults(page: Page, overrides: ApiMock = async () => {}): Promise<void> {
  await mockApi(page, async (route, request) => {
    const pathname = apiPath(request.url());
    const method = request.method();
    if (method === "GET" && pathname === "/api/v1/me") {
      await fulfillJson(route, 200, ME);
      return;
    }
    if (method === "GET" && pathname === "/api/v1/friends") {
      await fulfillJson(route, 200, { items: [] });
      return;
    }
    if (method === "GET" && pathname === "/api/v1/friends/locations") {
      await fulfillJson(route, 200, { items: [] });
      return;
    }
    if (method === "GET" && pathname === "/api/v1/encounters") {
      await fulfillJson(route, 200, { items: [] });
      return;
    }
    await overrides(route, request);
  });
}

test.describe("nearby", () => {
  test("renders a group bubble from GET /groups/nearby", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockDefaults(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/nearby") {
        await fulfillJson(route, 200, NEARBY);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await expect(page.getByText("情報工学科 3年", { exact: true })).toBeVisible();
    await expect(page.getByText("12人", { exact: true })).toBeVisible();
  });

  test("opens the group create screen from the + bubble", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockDefaults(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/nearby") {
        await fulfillJson(route, 200, { inside: [], near: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");
    await page.getByLabel("グループを作る").click();

    await page.waitForURL(/\/group\/new\/?$/);
  });

  test("shows the location CTA when GET /groups/nearby is 403 location_stale", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());

    await mockDefaults(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/nearby") {
        await fulfillJson(route, 403, {
          error: { code: "location_stale", message: "位置が古いです" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await expect(
      page.getByText("グループを見るには位置情報が必要です"),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "位置を有効にする" }),
    ).toBeVisible();
  });

  test("shows the empty hint when nothing is nearby", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockDefaults(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/nearby") {
        await fulfillJson(route, 200, { inside: [], near: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await expect(page.getByText("まだ近くに誰もいません")).toBeVisible();
  });

  test("shows a load error when GET /groups/nearby is 500", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockDefaults(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/nearby") {
        await fulfillJson(route, 500, {
          error: { code: "internal", message: "サーバーエラー" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await expect(
      page.getByText("近くのグループを読み込めませんでした"),
    ).toBeVisible();
  });

  test("asks before joining a group the viewer is not in", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());

    await mockDefaults(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/nearby") {
        await fulfillJson(route, 200, NEARBY);
        return;
      }
      if (method === "GET" && pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP_G1);
        return;
      }
      if (method === "POST" && pathname === "/api/v1/groups/g1/join") {
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    // 起動スプラッシュ (約 2 秒) が最初のポインタ操作を吸うので、消えるまで待つ。
    await page
      .locator('[class*="StartupSplash"]')
      .first()
      .waitFor({ state: "detached", timeout: 10_000 });

    // まだ入っていないグループの泡をタップすると、参加の確認が出る。
    const bubble = page.getByText("情報工学科 3年", { exact: true });
    await expect(bubble).toBeVisible();
    await bubble.click();

    const dialog = page.getByRole("alertdialog");
    await expect(dialog).toContainText("「情報工学科 3年」に参加しますか？");

    // キャンセルでは何も送らずに閉じる。
    await dialog.getByRole("button", { name: "キャンセル" }).click();
    await expect(dialog).toHaveCount(0);

    await bubble.click();
    const joinRequest = page.waitForRequest(
      (request) =>
        request.method() === "POST" &&
        apiPath(request.url()) === "/api/v1/groups/g1/join",
    );
    await page.getByRole("alertdialog").getByRole("button", { name: "参加する" }).click();

    const request = await joinRequest;
    expect(apiPath(request.url())).toBe("/api/v1/groups/g1/join");
    // 参加できたらそのグループの画面へ移る。
    await expect(page).toHaveURL(/\/group\/\?id=g1/);
  });

  test("tries to join a group outside the area and explains the refusal", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let joinCalls = 0;
    await mockDefaults(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/nearby") {
        await fulfillJson(route, 200, {
          inside: [],
          near: [{ id: "g2", name: "朝のランニング部", memberCount: 5, distanceBucket: "<500m" }],
        });
        return;
      }
      if (method === "POST" && pathname === "/api/v1/groups/g2/join") {
        joinCalls += 1;
        await fulfillJson(route, 403, {
          error: { code: "not_in_geofence", message: "エリアの外です" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");
    const bubble = page.getByText("朝のランニング部", { exact: true });
    await expect(bubble).toBeVisible();
    await bubble.click();

    const dialog = page.getByRole("alertdialog");
    await expect(dialog).toContainText("「朝のランニング部」に参加しますか？");
    await dialog.getByRole("button", { name: "参加する" }).click();
    await expect(dialog).toContainText("エリアの外にいるため参加できませんでした");
    expect(joinCalls).toBe(1);
    await expect(page).toHaveURL(/\/$/);
  });

  test("does not ask again for a near group the viewer already belongs to", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockDefaults(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/nearby") {
        // near には isMember が無い。詳細 (GET /groups/{id}) の isMember で判定する
        await fulfillJson(route, 200, {
          inside: [],
          near: [{ id: "g3", name: "写真部", memberCount: 8, distanceBucket: "<2km" }],
        });
        return;
      }
      if (method === "GET" && pathname === "/api/v1/groups/g3") {
        await fulfillJson(route, 200, {
          id: "g3",
          name: "写真部",
          description: "",
          visibility: "open",
          memberCount: 8,
          isMember: true,
          radiusM: 200,
          centerLat: 35.01,
          centerLon: 135,
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    // 詳細が届いてメンバーと分かるまで待ってからタップする
    const detail = page.waitForResponse(
      (response) => apiPath(response.url()) === "/api/v1/groups/g3",
    );
    await page.goto("/");
    const bubble = page.getByText("写真部", { exact: true });
    await expect(bubble).toBeVisible();
    await detail;
    await page.waitForTimeout(300);
    await bubble.click();
    await page.waitForTimeout(800);
    await expect(page.getByRole("alertdialog")).toHaveCount(0);
  });
});
