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

/** GET /me が返す平坦な meResponse (backend/internal/controller/me.go)。 */
const ME = {
  did: "did:plc:testuser",
  handle: "test.bsky.social",
  displayName: "てすとたろう",
  description: "近所のカフェでもくもくしています。",
  status: "カフェでもくもく中",
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

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-7-chain-fe-be-gap-closure.png",
);

const STATUS_EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-13-chain-fe-be-gap-closure.png",
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

test.describe("profile", () => {
  test("renders name, bio and status from GET /me", async ({ page }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/me") {
        await fulfillJson(route, 200, ME);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/profile/");

    await expect(page.getByRole("heading", { name: ME.displayName })).toBeVisible();
    await expect(page.getByText(ME.description)).toBeVisible();
    await expect(page.getByText(ME.status)).toBeVisible();
    await expect(page.getByText("まだメモリーはありません")).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }
  });

  test("shows the unavailable state when GET /me is 404", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/profile/");

    await expect(page.getByText("プロフィールは準備中です")).toBeVisible();
  });

  test("shows the error state with a retry when GET /me fails", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route) => {
      await fulfillJson(route, 500, {
        error: { code: "internal", message: "boom" },
      });
    });

    await page.goto("/profile/");

    await expect(
      page.getByText("プロフィールを読み込めませんでした"),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "再読み込み" })).toBeVisible();
  });
});

test.describe("profile auth", () => {
  test("signed-out visitor is sent from /profile/ to /welcome/", async ({ page }) => {
    await page.goto("/profile/");

    await page.waitForURL((url) => url.pathname.startsWith("/welcome"));
    await expect(
      page.getByRole("button", { name: "Blueskyで続ける" }),
    ).toBeVisible();
  });
});

/** /me, PUT /me/status, GET /feed をモックする。statuses には送った body を積む。 */
async function mockProfileApi(
  page: Page,
  opts: { statusStatus?: number; statusMessage?: string; feed?: unknown } = {},
): Promise<{ statuses: { status: string }[] }> {
  const statuses: { status: string }[] = [];
  await mockApi(page, async (route, request) => {
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();

    if (path === "/api/v1/me" && method === "GET") {
      await fulfillJson(route, 200, ME);
      return;
    }
    if (path === "/api/v1/me/status" && method === "PUT") {
      const body = request.postDataJSON() as { status: string };
      statuses.push(body);
      if (opts.statusStatus && opts.statusStatus >= 400) {
        await fulfillJson(route, opts.statusStatus, {
          error: {
            code: "moderation_block",
            message: opts.statusMessage ?? "blocked",
          },
        });
        return;
      }
      await fulfillJson(route, 200, { status: body.status });
      return;
    }
    if (path === "/api/v1/feed") {
      await fulfillJson(route, 200, opts.feed ?? { items: [] });
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });
  return { statuses };
}

test.describe("profile status edit", () => {
  test("saves the status via PUT /me/status and updates the display", async ({
    page,
  }, testInfo) => {
    await useStoredSession(page, makeStoredSession());
    const { statuses } = await mockProfileApi(page);
    await page.goto("/profile/");

    await expect(page.getByText(ME.status)).toBeVisible();

    await page.getByRole("button", { name: "編集", exact: true }).click();
    await page.getByLabel("ステータス").fill("旅の途中");
    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("旅の途中")).toBeVisible();
    await expect(page.getByText(ME.status)).toHaveCount(0);
    expect(statuses).toEqual([{ status: "旅の途中" }]);

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(STATUS_EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: STATUS_EVIDENCE_PATH, fullPage: true });
    }
  });

  test("shows the moderation error when PUT /me/status is rejected", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());
    await mockProfileApi(page, {
      statusStatus: 422,
      statusMessage: "投稿内容が不適切です",
    });
    await page.goto("/profile/");

    await page.getByRole("button", { name: "編集", exact: true }).click();
    await page.getByLabel("ステータス").fill("不適切な文言");
    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("投稿内容が不適切です")).toBeVisible();
    // 失敗時は編集を開いたままにして、入力を残す。
    await expect(page.getByLabel("ステータス")).toBeVisible();
  });

  test("lists the user's public posts from GET /feed?source=user:{did}", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());
    await mockProfileApi(page, {
      feed: {
        items: [
          {
            kind: "public",
            uri: "at://did:plc:testuser/jp.i32.chain.feed.post/1",
            author: { did: "did:plc:testuser", handle: "test.bsky.social" },
            text: "今日はいい天気",
            createdAt: "2026-10-17T00:00:00.000Z",
          },
        ],
      },
    });
    await page.goto("/profile/");

    const section = page.locator("section", { hasText: "公開投稿" });
    await expect(section.getByText("今日はいい天気")).toBeVisible();
  });
});

/** アイコン・バナーのある meResponse。画像の差し替え / 削除の表示確認に使う。 */
const ME_WITH_IMAGES = {
  ...ME,
  avatarUrl: "https://cdn.example/avatar.jpg",
  bannerUrl: "https://cdn.example/banner.jpg",
};

/** 1x1 の PNG。multipart の file として送れる最小の有効な画像。 */
const PNG_1X1 = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
  "base64",
);

type EditMockOptions = {
  me?: unknown;
  profileStatus?: number;
  profileMessage?: string;
  avatarStatus?: number;
  avatarMessage?: string;
};

/**
 * /me, PATCH /me/profile, PUT /me/avatar, DELETE /me/banner, GET /feed をモックする。
 * 送られた PATCH body と各リクエストを記録して返す。
 */
async function mockEditApi(
  page: Page,
  opts: EditMockOptions = {},
): Promise<{
  patches: { displayName?: string; description?: string }[];
  avatarContentType: string | null;
  deletedBanner: boolean;
}> {
  const patches: { displayName?: string; description?: string }[] = [];
  const state = { avatarContentType: null as string | null, deletedBanner: false };
  await mockApi(page, async (route, request) => {
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();

    if (path === "/api/v1/me" && method === "GET") {
      await fulfillJson(route, 200, opts.me ?? ME);
      return;
    }
    if (path === "/api/v1/me/profile" && method === "PATCH") {
      const body = request.postDataJSON() as { displayName: string; description: string };
      patches.push(body);
      if (opts.profileStatus && opts.profileStatus >= 400) {
        await fulfillJson(route, opts.profileStatus, {
          error: {
            code: "invalid_request",
            message: opts.profileMessage ?? "不正なリクエストです",
          },
        });
        return;
      }
      await fulfillJson(route, 200, {
        ...ME,
        displayName: body.displayName,
        description: body.description,
      });
      return;
    }
    if (path === "/api/v1/me/avatar" && method === "PUT") {
      state.avatarContentType = request.headers()["content-type"] ?? null;
      if (opts.avatarStatus && opts.avatarStatus >= 400) {
        await fulfillJson(route, opts.avatarStatus, {
          error: { code: "internal", message: opts.avatarMessage ?? "boom" },
        });
        return;
      }
      await fulfillJson(route, 200, ME_WITH_IMAGES);
      return;
    }
    if (path === "/api/v1/me/banner" && method === "DELETE") {
      state.deletedBanner = true;
      await fulfillJson(route, 200, ME);
      return;
    }
    if (path === "/api/v1/feed") {
      await fulfillJson(route, 200, { items: [] });
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });
  return {
    get patches() {
      return patches;
    },
    get avatarContentType() {
      return state.avatarContentType;
    },
    get deletedBanner() {
      return state.deletedBanner;
    },
  };
}

test.describe("profile edit", () => {
  test("saves the display name and description via PATCH /me/profile", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());
    const api = await mockEditApi(page);
    await page.goto("/profile/");

    await page.getByRole("button", { name: "プロフィールを編集" }).click();
    await page.getByLabel("名前").fill("あたらしい名前");
    await page.getByLabel("自己紹介").fill("よろしくおねがいします");
    await page.getByRole("button", { name: "保存" }).click();

    await expect(
      page.getByRole("heading", { name: "あたらしい名前" }),
    ).toBeVisible();
    await expect(page.getByText("よろしくおねがいします")).toBeVisible();
    await expect(page.getByText("プロフィールを更新しました")).toBeVisible();
    expect(api.patches).toEqual([
      { displayName: "あたらしい名前", description: "よろしくおねがいします" },
    ]);
  });

  test("uploads the avatar as multipart via PUT /me/avatar", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const api = await mockEditApi(page);
    await page.goto("/profile/");

    await page.setInputFiles('[data-testid="profile-avatar-input"]', {
      name: "avatar.png",
      mimeType: "image/png",
      buffer: PNG_1X1,
    });

    await expect(page.getByText("アイコンを更新しました")).toBeVisible();
    expect(api.avatarContentType).toContain("multipart/form-data");
  });

  test("removes the banner via DELETE /me/banner", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const api = await mockEditApi(page, { me: ME_WITH_IMAGES });
    await page.goto("/profile/");

    await page.getByRole("button", { name: "プロフィールを編集" }).click();
    await page.getByRole("button", { name: "バナーを削除" }).click();

    await expect(page.getByText("バナーを削除しました")).toBeVisible();
    expect(api.deletedBanner).toBe(true);
  });

  test("shows the error and keeps the editor open when PATCH /me/profile is 400", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());
    await mockEditApi(page, {
      profileStatus: 400,
      profileMessage: "64 文字までです",
    });
    await page.goto("/profile/");

    await page.getByRole("button", { name: "プロフィールを編集" }).click();
    await page.getByLabel("名前").fill("x");
    await page.getByRole("button", { name: "保存" }).click();

    await expect(
      page.getByRole("alert").filter({ hasText: "64 文字までです" }),
    ).toBeVisible();
    await expect(page.getByLabel("名前")).toBeVisible();
  });

  test("shows an error when PUT /me/avatar fails with 500", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockEditApi(page, { avatarStatus: 500, avatarMessage: "保存できません" });
    await page.goto("/profile/");

    await page.setInputFiles('[data-testid="profile-avatar-input"]', {
      name: "avatar.png",
      mimeType: "image/png",
      buffer: PNG_1X1,
    });

    await expect(
      page.getByRole("alert").filter({ hasText: "保存できません" }),
    ).toBeVisible();
  });
});
