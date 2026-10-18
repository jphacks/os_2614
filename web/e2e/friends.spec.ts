import { mkdirSync } from "node:fs";
import path from "node:path";
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

type StoredSession = {
  did: string;
  accessToken: string;
  refreshToken: string;
  accessExpiresAtMs: number;
};

type ApiMock = (route: Route, request: Request) => Promise<void> | void;

/** GET /friends (07 §2.3) が返す friendResponse のページ封筒。 */
const FRIENDS = {
  items: [
    {
      did: "did:plc:alice",
      handle: "alice.bsky.social",
      displayName: "アリス",
      since: "2026-09-01T00:00:00Z",
      precision: "area",
      met: true,
      via: "encounter",
    },
    {
      did: "did:plc:bob",
      handle: "bob.bsky.social",
      since: "2026-09-02T00:00:00Z",
      precision: "exact",
      met: true,
      via: "invite",
    },
  ],
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-20-chain-fe-be-gap-closure.png",
);

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

/** JWT 風の文字列。session.ts は payload の iat / exp だけを読む。 */
function fakeJwt(claims: Record<string, unknown>): string {
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify(claims));
  return `${header}.${payload}.signature`;
}

function jwtFor(did: string): string {
  const now = Math.floor(Date.now() / 1000);
  return fakeJwt({ sub: did, iat: now, exp: now + 900 });
}

function makeStoredSession(expiresInMs = 60 * 60 * 1000): StoredSession {
  const did = "did:plc:testuser";
  return {
    did,
    accessToken: jwtFor(did),
    refreshToken: "refresh-token-0",
    accessExpiresAtMs: Date.now() + expiresInMs,
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

/** chain.i32.jp への API 呼び出しをモックする。CORS プリフライトも同じ場所で返す。 */
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

/** パスを比較しやすいよう、pct-encoding を戻した pathname にする。 */
function apiPath(url: string): string {
  return decodeURIComponent(new URL(url).pathname);
}

test.describe("friends", () => {
  test("renders friends from GET /friends", async ({ page }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/friends") {
        await fulfillJson(route, 200, FRIENDS);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/friends/");

    await expect(page.getByText("位置はエリア名だけ共有しています")).toBeVisible();
    await expect(page.getByText("アリス", { exact: true })).toBeVisible();
    await expect(page.getByText("@alice.bsky.social", { exact: true })).toBeVisible();
    await expect(page.getByText("エリアのみ", { exact: true })).toBeVisible();
    await expect(page.getByText("@bob.bsky.social", { exact: true })).toBeVisible();
    await expect(page.getByText("正確な位置", { exact: true })).toBeVisible();
    // GET /friends/locations は未実装なので、位置は準備中で出す。
    await expect(page.getByText("位置は準備中")).toHaveCount(2);
    await expect(page.getByText("2人", { exact: true })).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }
  });

  test("delete issues DELETE /friends/{did} and hides the row", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/friends") {
        await fulfillJson(route, 200, FRIENDS);
        return;
      }
      if (method === "DELETE" && pathname === "/api/v1/friends/did:plc:alice") {
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/friends/");
    await expect(page.getByText("アリス", { exact: true })).toBeVisible();

    const deleteRequest = page.waitForRequest(
      (request) =>
        request.method() === "DELETE" &&
        apiPath(request.url()) === "/api/v1/friends/did:plc:alice",
    );

    await page.getByRole("button", { name: "アリスを友達から削除" }).click();

    const request = await deleteRequest;
    expect(apiPath(request.url())).toBe("/api/v1/friends/did:plc:alice");

    // 楽観更新: 成功後もアリスの行は消えたまま。
    await expect(page.getByText("アリス", { exact: true })).toHaveCount(0);
    await expect(page.getByText("1人", { exact: true })).toBeVisible();
  });

  test("rolls back the row when DELETE fails", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/friends") {
        await fulfillJson(route, 200, FRIENDS);
        return;
      }
      if (method === "DELETE") {
        await fulfillJson(route, 500, {
          error: { code: "internal", message: "boom" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/friends/");
    await expect(page.getByText("アリス", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "アリスを友達から削除" }).click();

    await expect(
      page.getByText("アリスさんを削除できませんでした。もう一度お試しください。"),
    ).toBeVisible();
    // 失敗したら行が戻る。
    await expect(page.getByText("アリス", { exact: true })).toBeVisible();
  });

  test("shows the explanation when GET /friends is empty", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/friends") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/friends/");

    await expect(page.getByText("まだ友達はいません")).toBeVisible();
    await expect(
      page.getByText(/Bluesky で相互フォローになった人が並びます/),
    ).toBeVisible();
  });

  test("hides the location settings for a friend who is not met (D6)", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const pathname = apiPath(request.url());
      if (request.method() === "GET" && pathname === "/api/v1/friends") {
        await fulfillJson(route, 200, {
          items: [
            ...FRIENDS.items,
            {
              did: "did:plc:carol",
              handle: "carol.bsky.social",
              displayName: "キャロル",
              since: "2026-09-03T00:00:00Z",
              precision: "off",
              met: false,
              via: null,
            },
          ],
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/friends/");
    await expect(page.getByText("キャロル", { exact: true })).toBeVisible();
    await expect(page.getByText("Bluesky で相互フォロー")).toBeVisible();
    // 会った友達には位置の見せ方を出し、会っていない友達には出さない。
    await expect(page.getByRole("button", { name: "アリスの位置の見せ方" })).toBeVisible();
    await expect(page.getByRole("button", { name: "キャロルの位置の見せ方" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "キャロルを友達から削除" })).toBeVisible();
  });
});
