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

const PAGE_ONE = {
  items: [
    {
      kind: "public_post",
      uri: "at://did:plc:alice/app.bsky.feed.post/1",
      author: { did: "did:plc:alice", handle: "alice.bsky.social", displayName: "アリス" },
      text: "はじめまして",
      createdAt: "2026-10-05T09:00:00Z",
    },
  ],
  cursor: "c2",
};

const PAGE_TWO = {
  items: [
    {
      kind: "public_post",
      uri: "at://did:plc:bob/app.bsky.feed.post/2",
      author: { did: "did:plc:bob", handle: "bob.bsky.social", displayName: "ボブ" },
      text: "こんにちは",
      createdAt: "2026-10-04T09:00:00Z",
    },
  ],
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-37-chain-fe-be-gap-closure.png",
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

function apiPath(url: string): string {
  return decodeURIComponent(new URL(url).pathname);
}

test.describe("home feed", () => {
  test("renders newest posts and appends the next page", async ({ page }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/feed") {
        const cursor = new URL(request.url()).searchParams.get("cursor");
        await fulfillJson(route, 200, cursor === "c2" ? PAGE_TWO : PAGE_ONE);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/home/");

    await expect(page.getByText("はじめまして")).toBeVisible();
    await expect(page.getByText("アリス", { exact: true })).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }

    await page.getByRole("button", { name: "もっと見る" }).click();

    await expect(page.getByText("こんにちは")).toBeVisible();
    await expect(page.getByText("はじめまして")).toBeVisible();
  });

  test("switches to the groups source and labels each post with its space", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    const sources: (string | null)[] = [];
    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/feed") {
        const source = new URL(request.url()).searchParams.get("source");
        sources.push(source);
        await fulfillJson(
          route,
          200,
          source === "groups"
            ? {
                items: [
                  {
                    kind: "instance_post",
                    uri: "at://did:web:chain.i32.jp/space/g1/did:plc:carol/jp.i32.chain.instance.post/3",
                    space: { id: "g1", type: "group", name: "情報工学科 3年" },
                    author: { did: "did:plc:carol", handle: "carol.bsky.social", displayName: "キャロル" },
                    text: "3限おわった",
                    createdAt: "2026-10-05T10:00:00Z",
                  },
                ],
              }
            : PAGE_ONE,
        );
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/home/");
    await expect(page.getByText("はじめまして")).toBeVisible();

    await page.getByRole("radio", { name: "グループ" }).click();

    await expect(page.getByText("3限おわった")).toBeVisible();
    await expect(page.getByRole("link", { name: "情報工学科 3年" })).toHaveAttribute(
      "href",
      "/group/?id=g1",
    );
    await expect(page.getByText("はじめまして")).toHaveCount(0);
    expect(sources).toEqual(["home", "groups"]);
  });

  test("creates a standalone public post from compose via POST /feed/posts", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let body: unknown;
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (pathname === "/api/v1/feed" && method === "GET") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      if (pathname === "/api/v1/instances") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      if (pathname === "/api/v1/feed/posts" && method === "POST") {
        body = request.postDataJSON();
        await fulfillJson(route, 201, {
          uri: "at://did:plc:testuser/app.bsky.feed.post/9",
          cid: "bafy",
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/home/");
    await page.getByRole("button", { name: "投稿する" }).click();
    await expect(page).toHaveURL(/\/compose\/\?target=public$/);

    await expect(page.getByRole("button", { name: "公開", exact: true, pressed: true })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await page.getByLabel("投稿本文").fill("公開テスト");
    await page.getByRole("button", { name: "投稿する" }).click();

    await expect.poll(() => body).toEqual({ text: "公開テスト" });
    await expect(page).toHaveURL(/\/home\/?$/);
  });

  test("shows the empty explanation when the feed is empty", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/feed") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/home/");

    await expect(page.getByText("まだ投稿がありません")).toBeVisible();
  });

  test("switches to the nearby source and renders the nearby posts", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    const sources: (string | null)[] = [];
    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/feed") {
        const source = new URL(request.url()).searchParams.get("source");
        sources.push(source);
        await fulfillJson(
          route,
          200,
          source === "nearby"
            ? {
                items: [
                  {
                    kind: "nearby_post",
                    uri: "at://did:plc:dave/app.bsky.feed.post/4",
                    author: {
                      did: "did:plc:dave",
                      handle: "dave.bsky.social",
                      displayName: "デイブ",
                    },
                    text: "この場にいるよ",
                    createdAt: "2026-10-05T11:00:00Z",
                  },
                ],
              }
            : PAGE_ONE,
        );
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/home/");
    await expect(page.getByText("はじめまして")).toBeVisible();

    const nearby = page.getByRole("radio", { name: "近く" });
    await expect(nearby).toBeVisible();
    await nearby.click();

    await expect(page.getByText("この場にいるよ")).toBeVisible();
    await expect(page.getByText("はじめまして")).toHaveCount(0);
    expect(sources).toEqual(["home", "nearby"]);
  });

  test("shows the nearby empty explanation when the nearby feed is empty", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    const sources: (string | null)[] = [];
    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/feed") {
        const source = new URL(request.url()).searchParams.get("source");
        sources.push(source);
        await fulfillJson(route, 200, source === "nearby" ? { items: [] } : PAGE_ONE);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/home/");
    await expect(page.getByText("はじめまして")).toBeVisible();

    await page.getByRole("radio", { name: "近く" }).click();

    await expect(
      page.getByText("今この場にいる人の投稿がここに並びます。", { exact: true }),
    ).toBeVisible();
    expect(sources).toEqual(["home", "nearby"]);
  });
});
