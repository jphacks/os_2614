import { expect, test, type Page, type Request, type Route } from "@playwright/test";

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

/** GET /me が返す meResponse (backend/internal/controller/me.go)。 */
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

type Extra = { moderation?: unknown; hidden?: string };

/** GET /instances/{spaceId}/posts の 1 件 (service.PostItem、10 §4.2)。 */
function post(
  uri: string,
  displayName: string,
  text: string,
  extra: Extra = {},
) {
  return {
    kind: "post",
    uri,
    spaceId: "g1",
    author: { did: `did:plc:${uri}`, handle: `${uri}.bsky.social`, displayName },
    text,
    format: "plain",
    media: [],
    createdAt: "2026-10-17T02:30:00.000Z",
    ...extra,
  };
}

const PLAIN = post("plain", "けんた", "ふつうの投稿です。");
const WARN = post("warn", "みさき", "きつい言葉の投稿", {
  moderation: { label: "warn", display: "blur", categories: ["harassment"] },
});
const BLOCK = post("block", "たかし", "非表示になる投稿", {
  hidden: "moderation",
});

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

/** group タイムラインの共通 mock。posts を差し替えて使う。 */
async function mockGroup(page: Page, items: unknown[]): Promise<void> {
  await mockApi(page, async (route, request) => {
    const method = request.method();
    const pathname = apiPath(request.url());
    if (method === "GET" && pathname === "/api/v1/groups/g1") {
      await fulfillJson(route, 200, GROUP);
      return;
    }
    if (method === "GET" && pathname === "/api/v1/instances/g1/posts") {
      await fulfillJson(route, 200, { items });
      return;
    }
    if (method === "GET" && pathname === "/api/v1/instances/g1/pins") {
      await fulfillJson(route, 200, { items: [] });
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });
}

test.describe("moderation display directives", () => {
  test("annotates a warn post and drops a block post (11 §4)", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockGroup(page, [PLAIN, WARN, BLOCK]);

    await page.goto("/group/?id=g1");

    await expect(page.getByText(PLAIN.text)).toBeVisible();
    await expect(
      page.getByText("モデレーションにより警告が付いています"),
    ).toBeVisible();
    await expect(page.getByText("嫌がらせ")).toBeVisible();
    // block (hidden:"moderation") は一覧に出ない。
    await expect(page.getByText(BLOCK.text)).toHaveCount(0);
  });

  test("blurs a warn post until the viewer reveals it", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockGroup(page, [WARN]);

    await page.goto("/group/?id=g1");

    const reveal = page.getByRole("button", { name: "タップして表示" });
    await expect(reveal).toBeVisible();
    await reveal.click();
    await expect(reveal).toHaveCount(0);
    await expect(page.getByText(WARN.text)).toBeVisible();
  });

  test("drops a warn post when the directive is hide", async ({ page }) => {
    const hidden = post("warn2", "みさき", "隠される投稿", {
      moderation: { label: "warn", display: "hide", categories: ["violence"] },
    });
    await useStoredSession(page, makeStoredSession());
    await mockGroup(page, [PLAIN, hidden]);

    await page.goto("/group/?id=g1");

    await expect(page.getByText(PLAIN.text)).toBeVisible();
    await expect(page.getByText(hidden.text)).toHaveCount(0);
    await expect(
      page.getByText("モデレーションにより警告が付いています"),
    ).toHaveCount(0);
  });

  test("persists warnDisplay through PATCH /me/settings (11 §1)", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());

    const captured: { path: string; body: unknown }[] = [];
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/me") {
        await fulfillJson(route, 200, ME);
        return;
      }
      if (method === "PATCH" && pathname === "/api/v1/me/settings") {
        captured.push({ path: pathname, body: request.postDataJSON() });
        await fulfillJson(route, 200, ME.settings);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/settings/");

    const warn = page.locator("section", { hasText: "警告の表示方法" });
    await expect(warn.getByRole("radio", { name: "ぼかす" })).toHaveAttribute(
      "aria-checked",
      "true",
    );

    await warn.getByRole("radio", { name: "隠す" }).click();

    await expect
      .poll(() => captured.find((c) => c.path === "/api/v1/me/settings"))
      .toMatchObject({ body: { warnDisplay: "hide" } });
  });
});
