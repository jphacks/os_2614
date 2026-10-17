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

/** GET /groups/{id} (service.GroupDetail、09 §4)。group は Space なので id が spaceId。 */
const GROUP = {
  id: "g1",
  name: "情報工学科 3年",
  description: "3年生の情報交換の場です。",
  visibility: "open",
  memberCount: 8,
  isMember: true,
  state: "active",
  radiusM: 40,
};

/** GET /instances/{spaceId}/posts の 1 件 (service.PostItem、10 §4.2)。 */
function post(uri: string, displayName: string, text: string) {
  return {
    kind: "post",
    uri,
    spaceId: "g1",
    author: { did: `did:plc:${uri}`, handle: `${uri}.bsky.social`, displayName },
    text,
    format: "plain",
    media: [],
    createdAt: "2026-10-17T02:30:00.000Z",
  };
}

const POST_1 = "at://did:plc:kenta/jp.i32.chain.instance.post/1";
const POST_2 = "at://did:plc:misaki/jp.i32.chain.instance.post/2";
const PIN_RKEY = "pin1";
const PIN_URI = `at://did:plc:auth/jp.i32.chain.instance.pin/${PIN_RKEY}`;

const POSTS = {
  items: [
    post(POST_1, "けんた", "明日の集合は10時、正門前で！"),
    post(POST_2, "みさき", "資料を共有しました。"),
  ],
};

/** GET /instances/{spaceId}/pins の手動 Pin (service.Pin、10 §6)。 */
const MANUAL_PIN = {
  uri: PIN_URI,
  subject: POST_1,
  reason: "manual",
  pinnedBy: "did:plc:testuser",
  createdAt: "2026-10-17T03:00:00.000Z",
};

/** 自動 Pin (reason=auto、10 §6)。 */
const AUTO_PIN = {
  uri: "at://did:plc:auth/jp.i32.chain.instance.pin/auto1",
  subject: POST_2,
  reason: "auto",
  createdAt: "2026-10-17T02:40:00.000Z",
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-41-chain-fe-be-gap-closure.png",
);

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

/** パスを比較しやすいよう、pct-encoding を戻した pathname にする。 */
function apiPath(url: string): string {
  return decodeURIComponent(new URL(url).pathname);
}

test.describe("instance pins", () => {
  test("pins a post manually and unpins it (10 §6)", async ({ page }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    let pins: unknown[] = [];

    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP);
        return;
      }
      if (method === "GET" && pathname === "/api/v1/instances/g1/posts") {
        await fulfillJson(route, 200, POSTS);
        return;
      }
      if (method === "GET" && pathname === "/api/v1/instances/g1/pins") {
        await fulfillJson(route, 200, { items: pins });
        return;
      }
      if (method === "POST" && pathname === "/api/v1/instances/g1/pins") {
        pins = [MANUAL_PIN];
        await fulfillJson(route, 201, MANUAL_PIN);
        return;
      }
      if (
        method === "DELETE" &&
        pathname === `/api/v1/instances/g1/pins/${PIN_RKEY}`
      ) {
        pins = [];
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/?id=g1");
    await expect(page.getByText(GROUP.name)).toBeVisible();

    // ピンタブ: まだ Pin が無い。
    await page.getByRole("radio", { name: "ピン" }).click();
    await expect(page.getByText("まだピンされた投稿はありません")).toBeVisible();
    await expect(page.getByText("ピンする投稿を選ぶ")).toBeVisible();

    // 手動 Pin: POST の body に subject (投稿の at-uri) が載る。
    const pinRequest = page.waitForRequest(
      (request) =>
        request.method() === "POST" &&
        apiPath(request.url()) === "/api/v1/instances/g1/pins",
    );
    await page.getByRole("button", { name: "ピンする" }).first().click();

    const posted = await pinRequest;
    expect(posted.postDataJSON()).toEqual({ subject: POST_1 });

    // 一覧に手動 Pin が出る。
    await expect(page.getByText("手動ピン")).toBeVisible();
    await expect(page.getByText("明日の集合は10時、正門前で！")).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }

    // 解除: DELETE /instances/{spaceId}/pins/{rkey} に載る。
    const unpinRequest = page.waitForRequest(
      (request) =>
        request.method() === "DELETE" &&
        apiPath(request.url()) === `/api/v1/instances/g1/pins/${PIN_RKEY}`,
    );
    await page.getByRole("button", { name: "ピンを外す" }).click();

    const deleted = await unpinRequest;
    expect(apiPath(deleted.url())).toBe(`/api/v1/instances/g1/pins/${PIN_RKEY}`);

    // 楽観的に残さない: 解除後は空に戻る。
    await expect(page.getByText("まだピンされた投稿はありません")).toBeVisible();
    await expect(page.getByRole("button", { name: "ピンを外す" })).toHaveCount(0);
  });

  test("labels an auto pin from GET /instances/{spaceId}/pins", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP);
        return;
      }
      if (method === "GET" && pathname === "/api/v1/instances/g1/posts") {
        await fulfillJson(route, 200, POSTS);
        return;
      }
      if (method === "GET" && pathname === "/api/v1/instances/g1/pins") {
        await fulfillJson(route, 200, { items: [AUTO_PIN] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/?id=g1");
    await page.getByRole("radio", { name: "ピン" }).click();

    await expect(page.getByText("自動ピン")).toBeVisible();
    await expect(page.getByText("資料を共有しました。")).toBeVisible();
  });

  test("shows an error when pinning is rejected (10 §6)", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP);
        return;
      }
      if (method === "GET" && pathname === "/api/v1/instances/g1/posts") {
        await fulfillJson(route, 200, POSTS);
        return;
      }
      if (method === "GET" && pathname === "/api/v1/instances/g1/pins") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      if (method === "POST" && pathname === "/api/v1/instances/g1/pins") {
        await fulfillJson(route, 409, {
          error: { code: "conflict", message: "Pin の上限に達しています" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/?id=g1");
    await page.getByRole("radio", { name: "ピン" }).click();
    await page.getByRole("button", { name: "ピンする" }).first().click();

    await expect(page.getByText("Pin の上限に達しています")).toBeVisible();
  });
});
