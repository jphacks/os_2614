import { mkdirSync } from "node:fs";
import path from "node:path";
import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
  type WebSocketRoute,
} from "@playwright/test";

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";

const CORS_HEADERS: Record<string, string> = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,OPTIONS",
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

/** GET /groups/{id} の応答 (service.GroupDetail、09 §4)。group は Space なので id が spaceId。 */
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
function post(uri: string, displayName: string, text: string, createdAt: string) {
  return {
    kind: "post",
    uri,
    spaceId: "g1",
    author: { did: `did:plc:${uri}`, handle: `${uri}.bsky.social`, displayName },
    text,
    format: "plain",
    media: [],
    createdAt,
  };
}

const PAGE1 = {
  items: [
    post("p1", "けんた", "1ページ目の投稿です。", "2026-10-17T02:30:00.000Z"),
    post("p2", "みさき", "集合は10時です。", "2026-10-17T01:12:00.000Z"),
  ],
  cursor: "c2",
};

const PAGE2 = {
  items: [
    post("p3", "たかし", "2ページ目の投稿です。", "2026-10-16T23:40:00.000Z"),
  ],
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-16-chain-fe-be-gap-closure.png",
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

test.describe("group timeline", () => {
  test("appends the next page of posts when もっと見る is pressed", async ({
    page,
  }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP);
        return;
      }
      if (url.pathname === "/api/v1/instances/g1/posts") {
        expect(url.searchParams.get("roots")).toBe("1");
        const cursor = url.searchParams.get("cursor");
        await fulfillJson(route, 200, cursor === "c2" ? PAGE2 : PAGE1);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/?id=g1");

    // ヘッダーと 1 ページ目の投稿 (author 要約 + 本文)。
    await expect(page.getByText(GROUP.name)).toBeVisible();
    await expect(page.getByText(PAGE1.items[0].text)).toBeVisible();
    await expect(page.getByText(PAGE1.items[1].text)).toBeVisible();
    // 2 ページ目はまだ出ていない。
    await expect(page.getByText(PAGE2.items[0].text)).toHaveCount(0);

    await page.getByRole("button", { name: "もっと見る" }).click();

    // cursor で次のページを取得して末尾に足す。
    await expect(page.getByText(PAGE2.items[0].text)).toBeVisible();
    // 続きが無くなれば「もっと見る」は消える。
    await expect(page.getByRole("button", { name: "もっと見る" })).toHaveCount(0);

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }
  });

  test("shows an inline error with a retry when the posts API fails", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP);
        return;
      }
      await fulfillJson(route, 500, {
        error: { code: "internal", message: "boom" },
      });
    });

    await page.goto("/group/?id=g1");

    await expect(page.getByText("投稿を読み込めませんでした")).toBeVisible();
    await expect(page.getByRole("button", { name: "再読み込み" })).toBeVisible();
  });

  test("adds a realtime reply to the existing replyCount", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    let socket: WebSocketRoute | undefined;
    await page.routeWebSocket(/\/api\/v1\/ws$/, (ws) => {
      socket = ws;
      ws.onMessage((message) => {
        const frame = JSON.parse(String(message)) as { type: string };
        if (frame.type === "auth") {
          ws.send(JSON.stringify({ type: "ready", did: "did:plc:testuser" }));
        } else if (frame.type === "sub") {
          ws.send(JSON.stringify({ type: "subscribed", id: "test", space: "g1" }));
        }
      });
    });
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP);
        return;
      }
      if (url.pathname === "/api/v1/instances/g1/posts") {
        await fulfillJson(route, 200, {
          items: [{ ...PAGE1.items[0], replyCount: 5 }],
        });
        return;
      }
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });

    await page.goto("/group/?id=g1");
    const replyButton = page.getByRole("button", { name: "返信" }).first();
    await expect(replyButton.getByText("5")).toBeVisible();
    await expect.poll(() => socket !== undefined).toBeTruthy();
    socket?.send(JSON.stringify({
      type: "event",
      event: "post.created",
      space: "g1",
      data: {
        ...post("reply-1", "みさき", "返信です。", "2026-10-17T03:00:00.000Z"),
        reply: { root: "p1", parent: "p1" },
      },
    }));
    await expect(replyButton.getByText("6")).toBeVisible();
  });
});
