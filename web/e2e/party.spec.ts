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
  "access-control-allow-methods": "GET,POST,PATCH,OPTIONS",
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

/** GET /party/current が返す PartyDetail (service.PartyDetail、08 §6)。 */
const PARTY = {
  id: "party-1",
  name: "大学キャンパスパーティ",
  phase: "confirmed",
  locked: false,
  tentativeAt: "2026-10-17T03:00:00.000Z",
  confirmedAt: "2026-10-17T03:01:00.000Z",
  members: [
    {
      did: "did:plc:testuser",
      handle: "test.bsky.social",
      displayName: "てすとたろう",
      present: true,
      left: false,
    },
    {
      did: "did:plc:yui0000000000",
      handle: "yui.bsky.social",
      displayName: "ゆい",
      present: true,
      left: false,
    },
    {
      did: "did:plc:sota000000000",
      handle: "sota.bsky.social",
      displayName: "そうた",
      present: false,
      left: true,
    },
  ],
};

/** GET /instances/{spaceId}/posts が返す 1 件 (service.PostItem、10 §4)。 */
const PARTY_POSTS = {
  items: [
    {
      kind: "jp.i32.chain.instance.post",
      uri: "at://did:plc:yui0000000000/jp.i32.chain.instance.post/1",
      spaceId: "party-1",
      text: "集合写真とれた",
      createdAt: "2026-10-17T03:05:00.000Z",
      media: [
        { id: "m1", alt: "", url: "", thumbUrl: "", width: 1600, height: 1200 },
      ],
    },
  ],
};

const TENTATIVE_PARTY = { ...PARTY, phase: "tentative", confirmedAt: undefined };

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-21-chain-fe-be-gap-closure.png",
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

async function useStoredSession(page: Page): Promise<void> {
  const session = makeStoredSession();
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

test.describe("party", () => {
  test("renders the confirmed party from GET /party/current", async ({ page }, testInfo) => {
    await useStoredSession(page);

    await mockApi(page, async (route, request) => {
      const pathname = new URL(request.url()).pathname;
      if (pathname === "/api/v1/party/current") {
        await fulfillJson(route, 200, PARTY);
        return;
      }
      if (pathname === "/api/v1/instances/party-1/posts") {
        expect(new URL(request.url()).searchParams.get("roots")).toBe("1");
        await fulfillJson(route, 200, PARTY_POSTS);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/party/");

    await expect(
      page.getByRole("heading", { name: PARTY.name }),
    ).toBeVisible();
    await expect(page.getByText("本パーティ")).toBeVisible();
    await expect(page.getByText("ゆい", { exact: true })).toBeVisible();
    await expect(page.getByText("そうた", { exact: true })).toBeVisible();
    await expect(page.getByText("退出済み", { exact: true })).toBeVisible();
    await expect(page.getByText("1枚")).toBeVisible();
    await expect(page.getByText("集合写真とれた")).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }
  });

  test("lock toggle calls POST /parties/{id}/lock", async ({ page }) => {
    await useStoredSession(page);

    await mockApi(page, async (route, request) => {
      const { pathname } = new URL(request.url());
      if (pathname === "/api/v1/party/current") {
        await fulfillJson(route, 200, PARTY);
        return;
      }
      if (pathname === "/api/v1/instances/party-1/posts") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      if (pathname === "/api/v1/parties/party-1/lock" && request.method() === "POST") {
        await fulfillJson(route, 200, {});
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/party/");

    const toggle = page.getByRole("switch", { name: "パーティを固定" });
    await expect(toggle).toHaveAttribute("aria-checked", "false");

    const lockRequest = page.waitForRequest(
      (request) =>
        request.method() === "POST" &&
        new URL(request.url()).pathname === "/api/v1/parties/party-1/lock",
    );
    await toggle.click();
    await lockRequest;

    await expect(toggle).toHaveAttribute("aria-checked", "true");
  });

  test("tentative party hides compose and rename and disables lock", async ({ page }) => {
    await useStoredSession(page);

    await mockApi(page, async (route, request) => {
      const { pathname } = new URL(request.url());
      if (pathname === "/api/v1/party/current") {
        await fulfillJson(route, 200, TENTATIVE_PARTY);
        return;
      }
      if (pathname === "/api/v1/instances/party-1/posts") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/party/");

    await expect(page.getByRole("switch", { name: "パーティを固定" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    await expect(page.getByText("本パーティになると固定できます")).toBeVisible();
    await expect(page.getByRole("button", { name: "投稿する" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "パーティ名を変更" })).toHaveCount(0);
    await expect(page.getByText("本パーティになると、写真や投稿を共有できます")).toBeVisible();
  });

  test("shows the empty state when GET /party/current is null", async ({ page }) => {
    await useStoredSession(page);

    await mockApi(page, async (route) => {
      await fulfillJson(route, 200, null);
    });

    await page.goto("/party/");

    await expect(
      page.getByText("近くのフレンドと会うとパーティが始まります"),
    ).toBeVisible();
  });

  test("reloads after terminal and different-id party.updated events", async ({ page }) => {
    await useStoredSession(page);
    let currentCalls = 0;
    let socket: WebSocketRoute | undefined;
    await page.routeWebSocket(/\/api\/v1\/ws$/, (ws) => {
      socket = ws;
      ws.onMessage((message) => {
        if (JSON.parse(String(message)).type === "auth") {
          ws.send(JSON.stringify({ type: "ready", did: "did:plc:testuser" }));
        }
      });
    });
    await mockApi(page, async (route, request) => {
      const pathname = new URL(request.url()).pathname;
      if (pathname === "/api/v1/party/current") {
        currentCalls++;
        await fulfillJson(route, 200, currentCalls > 2 ? null : PARTY);
        return;
      }
      if (pathname === "/api/v1/instances/party-1/posts") {
        await fulfillJson(route, 200, PARTY_POSTS);
        return;
      }
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });
    await page.goto("/party/");
    await expect.poll(() => socket !== undefined).toBeTruthy();
    // ページの準備前にイベントを送ると取りこぼすので、party の表示を待つ。
    await expect(page.getByRole("heading", { name: PARTY.name })).toBeVisible();

    socket?.send(JSON.stringify({
      type: "event",
      event: "party.updated",
      space: "party-1",
      data: { id: "other-party", phase: "confirmed", locked: false, present: [] },
    }));
    await expect.poll(() => currentCalls).toBeGreaterThan(1);

    socket?.send(JSON.stringify({
      type: "event",
      event: "party.updated",
      space: "party-1",
      data: { id: "party-1", phase: "ended", locked: false, present: [] },
    }));
    await expect(page.getByText("パーティが終わりました。メモリーに残っています")).toBeVisible();
  });

  test("can reply from /party/replies/", async ({ page }) => {
    await useStoredSession(page);
    let posted: Record<string, unknown> | null = null;
    await mockApi(page, async (route, request) => {
      const pathname = new URL(request.url()).pathname;
      if (pathname === "/api/v1/party/current") {
        await fulfillJson(route, 200, PARTY);
        return;
      }
      if (pathname === "/api/v1/me") {
        await fulfillJson(route, 200, { did: "did:plc:testuser", handle: "test.bsky.social", displayName: "てすと" });
        return;
      }
      if (pathname === "/api/v1/instances") {
        await fulfillJson(route, 200, { items: [{ spaceId: "party-1", canWrite: true }] });
        return;
      }
      if (pathname === "/api/v1/instances/party-1/thread") {
        await fulfillJson(route, 200, {
          root: { ...PARTY_POSTS.items[0], uri: "post-1", author: { did: "did:plc:yui", handle: "yui", displayName: "ゆい" } },
          replies: [],
        });
        return;
      }
      if (pathname === "/api/v1/instances/party-1/posts") {
        await fulfillJson(route, 200, {
          items: [{ ...PARTY_POSTS.items[0], uri: "post-1", author: { did: "did:plc:yui", handle: "yui", displayName: "ゆい" } }],
        });
        return;
      }
      if (pathname === "/api/v1/posts" && request.method() === "POST") {
        posted = request.postDataJSON() as Record<string, unknown>;
        await fulfillJson(route, 201, { items: [{ uri: "reply-1", spaceId: "party-1" }] });
        return;
      }
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });

    await page.goto("/party/replies/?id=party-1&post=post-1");
    const dialog = page.getByRole("dialog", { name: "返信" });
    await expect(dialog).toBeVisible();
    await dialog.getByLabel("返信を書く").fill("party への返信");
    await dialog.getByRole("button", { name: "返信を送信" }).click();
    await expect.poll(() => posted).not.toBeNull();
    expect(posted).toMatchObject({
      spaceIds: ["party-1"],
      reply: { root: "post-1", parent: "post-1" },
    });
  });
});
