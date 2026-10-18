import { mkdirSync } from "node:fs";
import path from "node:path";
import { deflateSync } from "node:zlib";
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

const ME_EMPTY_STATUS = { ...ME, status: "" };

const STATS = {
  encounters: { today: 1, week: 8, month: 12, total: 1024, uniquePeople: 9 },
  friends: 8,
  parties: { total: 14 },
  groups: { member: 2 },
  posts: { month: 3 },
};

const MEMORY = {
  spaceId: "party-1",
  type: "party",
  name: "秋の集まり",
  from: "2026-10-01T00:00:00Z",
  until: "2026-10-01T02:00:00Z",
  memberCount: 4,
  mediaCount: 2,
  cover: { thumbUrl: "https://cdn.example/memory.jpg" },
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
    // ステータスは名前の直下の行に出る (縦に重ならず、名前より下)。
    const nameBox = await page.getByRole("heading", { name: ME.displayName }).boundingBox();
    const statusBox = await page.getByText(ME.status).boundingBox();
    expect(nameBox).not.toBeNull();
    expect(statusBox).not.toBeNull();
    expect(statusBox!.y).toBeGreaterThanOrEqual(nameBox!.y + nameBox!.height);
    await expect(page.getByText("まだメモリーはありません")).toBeVisible();
    await expect(page.getByRole("button", { name: "バナーを変更" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "アイコンを変更" })).toHaveCount(0);
    await expect(page.getByTestId("profile-image-overlay")).toHaveCount(0);

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

test.describe("profile overview resources", () => {
  test("renders stats and a memory card from their APIs", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route, request) => {
      const path = new URL(request.url()).pathname;
      if (path === "/api/v1/me") return fulfillJson(route, 200, ME);
      if (path === "/api/v1/me/stats") return fulfillJson(route, 200, STATS);
      if (path === "/api/v1/memories") return fulfillJson(route, 200, { items: [MEMORY] });
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });
    await page.goto("/profile/");
    await expect(page.getByText("1,024")).toBeVisible();
    await expect(page.getByText("8", { exact: true })).toBeVisible();
    await expect(page.getByText("14", { exact: true })).toBeVisible();
    await expect(page.getByText("秋の集まり")).toBeVisible();
  });

  test("renders dashes when stats is unavailable", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/me") return fulfillJson(route, 200, ME);
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });
    await page.goto("/profile/");
    await expect(page.getByText("—", { exact: true })).toHaveCount(3);
  });

  test("shows the empty status placeholder below the name and opens its editor", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/me") return fulfillJson(route, 200, ME_EMPTY_STATUS);
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });
    await page.goto("/profile/");
    const placeholder = page.getByRole("button", { name: "ステータスを設定" });
    await expect(placeholder).toBeVisible();
    const nameBox = await page.getByRole("heading", { name: ME.displayName }).boundingBox();
    const placeholderBox = await placeholder.boundingBox();
    expect(placeholderBox!.y).toBeGreaterThanOrEqual(nameBox!.y + nameBox!.height);
    await placeholder.click();
    const input = page.getByLabel("ステータス", { exact: true });
    await expect(input).toBeVisible();
    // 編集欄も名前の下に出る。
    const inputBox = await input.boundingBox();
    expect(inputBox!.y).toBeGreaterThanOrEqual(nameBox!.y + nameBox!.height);
  });

  test("saves a status from the empty state via PUT /me/status", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const { statuses } = await mockProfileApi(page, { me: () => ME_EMPTY_STATUS });
    await page.goto("/profile/");

    await page.getByRole("button", { name: "ステータスを設定" }).click();
    await page.getByLabel("ステータス", { exact: true }).fill("はじめまして");
    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("はじめまして")).toBeVisible();
    expect(statuses).toEqual([{ status: "はじめまして" }]);
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
  opts: {
    me?: unknown | (() => unknown);
    statusStatus?: number;
    statusMessage?: string;
    feed?: unknown;
  } = {},
): Promise<{ statuses: { status: string }[] }> {
  const statuses: { status: string }[] = [];
  await mockApi(page, async (route, request) => {
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();

    if (path === "/api/v1/me" && method === "GET") {
      const me = typeof opts.me === "function" ? opts.me() : opts.me;
      await fulfillJson(route, 200, me ?? ME);
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

    await page.getByRole("button", { name: "ステータスを編集" }).click();
    await page.getByLabel("ステータス", { exact: true }).fill("旅の途中");
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

    await page.getByRole("button", { name: "ステータスを編集" }).click();
    await page.getByLabel("ステータス", { exact: true }).fill("不適切な文言");
    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("投稿内容が不適切です")).toBeVisible();
    // 失敗時は編集を開いたままにして、入力を残す。
    await expect(page.getByLabel("ステータス", { exact: true })).toBeVisible();
  });

  test("saves with Enter and cancels with Escape", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const { statuses } = await mockProfileApi(page);
    await page.goto("/profile/");

    const input = page.getByLabel("ステータス", { exact: true });
    await page.getByRole("button", { name: "ステータスを編集" }).click();
    await input.fill("Enterで保存");
    await input.press("Enter");
    await expect(page.getByText("Enterで保存")).toBeVisible();
    expect(statuses).toEqual([{ status: "Enterで保存" }]);

    await page.getByRole("button", { name: "ステータスを編集" }).click();
    await page.getByLabel("ステータス", { exact: true }).fill("保存しない");
    await page.getByLabel("ステータス", { exact: true }).press("Escape");
    await expect(page.getByText("Enterで保存")).toBeVisible();
    await expect(page.getByLabel("ステータス", { exact: true })).toHaveCount(0);
    expect(statuses).toEqual([{ status: "Enterで保存" }]);
  });

  test("does not save when Enter confirms an IME composition", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const { statuses } = await mockProfileApi(page);
    await page.goto("/profile/");

    await page.getByRole("button", { name: "ステータスを編集" }).click();
    const input = page.getByLabel("ステータス", { exact: true });
    await input.evaluate((element) => {
      element.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Enter",
          bubbles: true,
          isComposing: true,
        }),
      );
    });

    await expect(input).toBeVisible();
    expect(statuses).toEqual([]);
  });

  test("wraps a long status to at most two lines and keeps it inside the screen", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 375, height: 812 });
    await useStoredSession(page, makeStoredSession());
    const responseMe = {
      ...ME,
      displayName: "とても長い名前のユーザーです",
      status: "あ".repeat(40),
    };
    await mockProfileApi(page, { me: () => responseMe });
    await page.goto("/profile/");

    const text = page.getByText(responseMe.status, { exact: true });
    await expect(text).toBeVisible();
    const badge = text.locator("..");
    const viewportWidth = page.viewportSize()?.width ?? 375;
    const badgeBox = await badge.boundingBox();
    expect(badgeBox).not.toBeNull();
    expect(badgeBox!.x).toBeGreaterThanOrEqual(0);
    expect(badgeBox!.x + badgeBox!.width).toBeLessThanOrEqual(viewportWidth);
    const pencilBox = await badge
      .getByRole("button", { name: "ステータスを編集" })
      .boundingBox();
    expect(pencilBox).not.toBeNull();
    expect(pencilBox!.x + pencilBox!.width).toBeLessThanOrEqual(viewportWidth);
    // 2 行までに収まる (caption の行高 x 2 + pill の余白に収まる)。
    const lineHeight = await text.evaluate((el) => parseFloat(getComputedStyle(el).lineHeight));
    expect((await text.boundingBox())!.height).toBeLessThanOrEqual(lineHeight * 2 + 1);

    await badge.getByRole("button", { name: "ステータスを編集" }).click();
    const statusInput = page.getByLabel("ステータス", { exact: true });
    const cancelButton = page.getByRole("button", { name: "キャンセル" });
    const saveButton = page.getByRole("button", { name: "保存" });
    const inputBox = await statusInput.boundingBox();
    expect(inputBox).not.toBeNull();
    expect(inputBox!.width).toBeGreaterThanOrEqual(120);
    for (const button of [cancelButton, saveButton]) {
      const buttonBox = await button.boundingBox();
      expect(buttonBox).not.toBeNull();
      expect(buttonBox!.x).toBeGreaterThanOrEqual(0);
      expect(buttonBox!.x + buttonBox!.width).toBeLessThanOrEqual(viewportWidth);
    }
  });

  test("aligns the inline status editor controls", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockProfileApi(page);
    await page.goto("/profile/");
    await page.getByRole("button", { name: "ステータスを編集" }).click();

    const input = page.getByLabel("ステータス", { exact: true });
    const counter = page.getByText(`${Array.from(ME.status).length}/40`, { exact: true });
    const cancel = page.getByRole("button", { name: "キャンセル" });
    const save = page.getByRole("button", { name: "保存" });
    const boxes = await Promise.all(
      [input, counter, cancel, save].map((locator) => locator.boundingBox()),
    );
    const centers = boxes.map((box) => (box?.y ?? 0) + (box?.height ?? 0) / 2);
    expect(Math.max(...centers) - Math.min(...centers)).toBeLessThanOrEqual(4);
  });

  test("does not show the public posts section on the profile tab", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());
    const feedRequests: string[] = [];
    await page.route("**/api/v1/feed*", (route) => {
      feedRequests.push(route.request().url());
      return route.fallback();
    });
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

    // 自分のプロフィールタブでは公開投稿を出さない (ui-v2 §4.7)。
    await expect(page.getByRole("heading", { name: ME.displayName })).toBeVisible();
    await expect(page.locator("section", { hasText: "公開投稿" })).toHaveCount(0);
    await expect(page.getByText("今日はいい天気")).toHaveCount(0);
    expect(feedRequests).toEqual([]);
  });
});

/** アイコン・バナーのある meResponse。画像の差し替え / 削除の表示確認に使う。 */
const ME_WITH_IMAGES = {
  ...ME,
  avatarUrl: "https://cdn.example/avatar.jpg",
  bannerUrl: "https://cdn.example/banner.jpg",
};

/** 4x4 RGB PNG。Chromium の createImageBitmap でも読める画像。 */
const PNG_4X4 = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAIAAAAmkwkpAAAAEElEQVR4nGM4UWEDRwzEcQBykhfBWhDcSAAAAABJRU5ErkJggg==",
  "base64",
);

function crc32(data: Buffer): number {
  let crc = 0xffffffff;
  for (const byte of data) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) {
      crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0);
    }
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function pngChunk(type: string, data: Buffer): Buffer {
  const typeBuffer = Buffer.from(type, "ascii");
  const body = Buffer.concat([typeBuffer, data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body), 0);
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length, 0);
  return Buffer.concat([length, body, crc]);
}

function makeRgbaPng(width: number, height: number): Buffer {
  const rows: Buffer[] = [];
  for (let y = 0; y < height; y += 1) {
    const row = Buffer.alloc(1 + width * 4);
    for (let x = 0; x < width; x += 1) {
      const offset = 1 + x * 4;
      row[offset] = 80 + (x % 120);
      row[offset + 1] = 120 + (y % 100);
      row[offset + 2] = 180;
      row[offset + 3] = 255;
    }
    rows.push(row);
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = 6;
  return Buffer.concat([
    Buffer.from("89504e470d0a1a0a", "hex"),
    pngChunk("IHDR", header),
    pngChunk("IDAT", deflateSync(Buffer.concat(rows))),
    pngChunk("IEND", Buffer.alloc(0)),
  ]);
}

const PNG_60X40 = makeRgbaPng(60, 40);

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
  requests: string[];
}> {
  const patches: { displayName?: string; description?: string }[] = [];
  const state = { avatarContentType: null as string | null, deletedBanner: false };
  const requests: string[] = [];
  await mockApi(page, async (route, request) => {
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();
    if (path !== "/api/v1/feed") {
      requests.push(`${method} ${path}`);
    }

    if (path === "/api/v1/me" && method === "GET") {
      await fulfillJson(route, 200, opts.me ?? ME);
      return;
    }
    if (path === "/api/v1/me/profile" && method === "PATCH") {
      const body = request.postDataJSON() as { displayName?: string; description?: string };
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
        ...body,
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
    get requests() {
      return requests;
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

    await page.getByRole("button", { name: "編集", exact: true }).click();
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

    await page.getByRole("button", { name: "編集", exact: true }).click();
    const chooser = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "アイコンを変更" }).click();
    await (await chooser).setFiles({
      name: "avatar.png",
      mimeType: "image/png",
      buffer: PNG_4X4,
    });

    const cropDialog = page.getByRole("dialog", { name: "アイコンを調整" });
    const cropImage = cropDialog.locator("img");
    await expect(cropDialog.getByRole("alert").filter({ hasText: "画像を読み込めませんでした" })).toHaveCount(0);
    await expect.poll(() => cropImage.evaluate((element: HTMLImageElement) => element.naturalWidth)).toBeGreaterThan(0);
    await cropDialog.getByRole("button", { name: "決定" }).click();
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByText("プロフィールを更新しました")).toBeVisible();
    expect(api.avatarContentType).toContain("multipart/form-data");
  });

  test("keeps the banner crop preview covering the frame while dragging", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockEditApi(page);
    await page.goto("/profile/");
    await page.getByRole("button", { name: "編集", exact: true }).click();
    const chooser = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "バナーを変更" }).click();
    await (await chooser).setFiles({
      name: "banner.png",
      mimeType: "image/png",
      buffer: PNG_60X40,
    });

    const cropDialog = page.getByRole("dialog", { name: "バナーを調整" });
    const frame = cropDialog.getByRole("application");
    const image = cropDialog.locator("img");
    await expect.poll(() => image.evaluate((element: HTMLImageElement) => element.naturalWidth)).toBeGreaterThan(0);
    const frameBox = await frame.boundingBox();
    expect(frameBox).not.toBeNull();
    const startX = frameBox!.x + frameBox!.width / 2;
    const startY = frameBox!.y + frameBox!.height / 2;
    await page.mouse.move(startX, startY);
    await page.mouse.down();
    await page.mouse.move(startX, startY + Math.min(200, frameBox!.height), { steps: 5 });
    await page.mouse.up();

    const imageBox = await image.boundingBox();
    expect(imageBox).not.toBeNull();
    expect(imageBox!.y).toBeLessThanOrEqual(frameBox!.y + 1);
    expect(imageBox!.y + imageBox!.height).toBeGreaterThanOrEqual(
      frameBox!.y + frameBox!.height - 1,
    );
    expect(imageBox!.x).toBeLessThanOrEqual(frameBox!.x + 1);
    expect(imageBox!.x + imageBox!.width).toBeGreaterThanOrEqual(
      frameBox!.x + frameBox!.width - 1,
    );
  });

  test("cancelling profile edit after cropping does not upload the avatar", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const api = await mockEditApi(page);
    await page.goto("/profile/");
    await page.getByRole("button", { name: "編集", exact: true }).click();
    const chooser = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "アイコンを変更" }).click();
    await (await chooser).setFiles({ name: "avatar.png", mimeType: "image/png", buffer: PNG_4X4 });
    await page.getByRole("dialog", { name: "アイコンを調整" }).getByRole("button", { name: "決定" }).click();
    await page.getByRole("button", { name: "キャンセル" }).last().click();
    await expect(page.getByLabel("名前")).toHaveCount(0);
    expect(api.requests.filter((request) => request === "PUT /api/v1/me/avatar")).toHaveLength(0);
  });

  test("removes the banner via DELETE /me/banner", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const api = await mockEditApi(page, { me: ME_WITH_IMAGES });
    await page.goto("/profile/");

    await page.getByRole("button", { name: "編集", exact: true }).click();
    await page.getByRole("button", { name: "バナーを変更" }).click();
    await page.getByRole("button", { name: "削除" }).click();
    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("プロフィールを更新しました")).toBeVisible();
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

    await page.getByRole("button", { name: "編集", exact: true }).click();
    await page.getByLabel("名前").fill("x");
    await page.getByRole("button", { name: "保存" }).click();

    await expect(
      page.getByRole("alert").filter({ hasText: "64 文字までです" }),
    ).toBeVisible();
    await expect(page.getByLabel("名前")).toBeVisible();
  });

  test("shows an error when PUT /me/avatar fails with 500", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const api = await mockEditApi(page, {
      avatarStatus: 500,
      avatarMessage: "保存できません",
    });
    await page.goto("/profile/");

    await page.getByRole("button", { name: "編集", exact: true }).click();
    await page.getByLabel("名前").fill("更新済み名前");
    const chooser = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "アイコンを変更" }).click();
    await (await chooser).setFiles({
      name: "avatar.png",
      mimeType: "image/png",
      buffer: PNG_4X4,
    });

    await page.getByRole("dialog", { name: "アイコンを調整" }).getByRole("button", { name: "決定" }).click();
    await page.getByRole("button", { name: "保存" }).click();
    await expect(
      page.getByRole("alert").filter({ hasText: "保存できません" }),
    ).toBeVisible();
    await expect(page.getByRole("heading", { name: "更新済み名前" })).toBeVisible();
    await expect(page.getByLabel("名前")).toHaveValue("更新済み名前");
    expect(api.requests.filter((request) => request === "PATCH /api/v1/me/profile")).toHaveLength(1);
    await page.getByRole("button", { name: "保存" }).click();
    await expect.poll(() => api.requests.filter((request) => request === "PUT /api/v1/me/avatar").length).toBe(2);
    expect(api.requests.filter((request) => request === "PATCH /api/v1/me/profile")).toHaveLength(1);
  });

  test("image-only change does not PATCH", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const api = await mockEditApi(page);
    await page.goto("/profile/");
    await page.getByRole("button", { name: "編集", exact: true }).click();
    const chooser = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "アイコンを変更" }).click();
    await (await chooser).setFiles({ name: "avatar.png", mimeType: "image/png", buffer: PNG_4X4 });
    await page.getByRole("dialog", { name: "アイコンを調整" }).getByRole("button", { name: "決定" }).click();
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByText("プロフィールを更新しました")).toBeVisible();
    expect(api.requests.indexOf("PATCH /api/v1/me/profile")).toBe(-1);
    expect(api.requests.indexOf("PUT /api/v1/me/avatar")).toBeGreaterThan(-1);
  });

  test("saves profile text before the avatar", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const api = await mockEditApi(page);
    await page.goto("/profile/");
    await page.getByRole("button", { name: "編集", exact: true }).click();
    await page.getByLabel("名前").fill("順序確認の名前");
    const chooser = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "アイコンを変更" }).click();
    await (await chooser).setFiles({ name: "avatar.png", mimeType: "image/png", buffer: PNG_4X4 });
    await page.getByRole("dialog", { name: "アイコンを調整" }).getByRole("button", { name: "決定" }).click();
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByText("プロフィールを更新しました")).toBeVisible();
    expect(api.requests.indexOf("PATCH /api/v1/me/profile")).toBeLessThan(api.requests.indexOf("PUT /api/v1/me/avatar"));
  });

  test("bio-only edit sends only the description field", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const api = await mockEditApi(page);
    await page.goto("/profile/");
    await page.getByRole("button", { name: "編集", exact: true }).click();
    await page.getByLabel("自己紹介").fill("自己紹介だけ更新");
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByText("プロフィールを更新しました")).toBeVisible();
    expect(api.patches).toEqual([{ description: "自己紹介だけ更新" }]);
  });

  test("refetches /me on a profile conflict and keeps the draft", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    let meGets = 0;
    await mockApi(page, async (route, request) => {
      const path = new URL(request.url()).pathname;
      if (path === "/api/v1/me" && request.method() === "GET") {
        meGets += 1;
        return fulfillJson(route, 200, ME);
      }
      if (path === "/api/v1/me/profile") {
        return fulfillJson(route, 409, { error: { code: "conflict", message: "conflict" } });
      }
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });
    await page.goto("/profile/");
    await page.getByRole("button", { name: "編集", exact: true }).click();
    await page.getByLabel("名前").fill("競合前の名前");
    await page.getByRole("button", { name: "保存" }).click();
    await expect(
      page.getByRole("alert").filter({ hasText: "他の場所で更新されたため" }),
    ).toBeVisible();
    await expect(page.getByLabel("名前")).toHaveValue("競合前の名前");
    expect(meGets).toBeGreaterThanOrEqual(2);
  });
});
