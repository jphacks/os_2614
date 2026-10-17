import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
} from "@playwright/test";

import { MAX_MEDIA_BYTES } from "../src/components/feature/compose/composeInput";

// Space への写真共有 (10 §4.4)。選んだ画像は multipart で POST /media へ送り、
// 201 {id,width,height} の id を POST /instances/{spaceId}/media の body に載せて
// 写真投稿を作る。プレビューは object URL。JPEG/PNG/WebP 以外と 15MB 超は
// アップロード前にクライアントで拒否し、API を 1 回も呼ばない。

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";

const CORS_HEADERS: Record<string, string> = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,OPTIONS",
  "access-control-allow-headers": "authorization,content-type",
  "access-control-max-age": "600",
};

/** 64x64 の単色 PNG。プレビュー (object URL) が実画像として描画されることを確認する。 */
const PNG_BYTES = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAIAAAAlC+aJAAAAT0lEQVR42u3PQQkAAAgEsCtjMuNbwgi+hcEKLNXzWgQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQELguYZjFanhwbHAAAAABJRU5ErkJggg==",
  "base64",
);

// Playwright 1.64.1 では fullyParallel 時に trace ファイルの後始末が
// browserContext.close で ENOENT になることがあるため、この spec は trace を切る。
test.use({ trace: "off" });

type StoredSession = {
  did: string;
  accessToken: string;
  refreshToken: string;
  accessExpiresAtMs: number;
};

type ApiMock = (route: Route, request: Request) => Promise<void> | void;
type Captured = { path: string; body: unknown };

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

/** JWT 風の文字列。session.ts は payload の iat / exp だけを読む。 */
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

function apiPath(url: string): string {
  return decodeURIComponent(new URL(url).pathname);
}

const NOT_FOUND = { error: { code: "not_found", message: "not found" } };

test.describe("space photo share", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("uploads via multipart POST /media then shares the mediaId to the space", async ({
    page,
  }) => {
    let mediaContentType: string | null = null;
    let mediaBody = "";
    let mediaCalls = 0;
    let shareCall: Captured | null = null;

    await mockApi(page, async (route, request) => {
      const pathname = apiPath(request.url());
      if (request.method() === "POST" && pathname === "/api/v1/media") {
        mediaCalls += 1;
        mediaContentType = request.headers()["content-type"] ?? null;
        mediaBody = request.postDataBuffer()?.toString("utf8") ?? "";
        await fulfillJson(route, 201, { id: "m1", width: 800, height: 600 });
        return;
      }
      if (
        request.method() === "POST" &&
        pathname === "/api/v1/instances/g1/media"
      ) {
        shareCall = { path: pathname, body: request.postDataJSON() };
        await fulfillJson(route, 201, {
          uri: "at://did:plc:testuser/jp.i32.chain.instance.media/m1",
          spaceId: "g1",
          mediaId: "m1",
        });
        return;
      }
      await fulfillJson(route, 404, NOT_FOUND);
    });

    await page.goto("/group/photo/?id=g1");
    await page.setInputFiles('[data-testid="space-photo-input"]', {
      name: "photo.png",
      mimeType: "image/png",
      buffer: PNG_BYTES,
    });

    // 選択しただけでは API を呼ばない。プレビューは object URL。
    await expect(page.locator('img[src^="blob:"]')).toBeVisible();
    expect(mediaCalls).toBe(0);

    await page.getByRole("button", { name: "共有する" }).click();

    await expect.poll(() => mediaContentType ?? "").toContain("multipart/form-data");
    expect(mediaCalls).toBe(1);
    expect(mediaBody).toContain('name="file"');
    expect(mediaBody).toContain('filename="photo.png"');

    await expect
      .poll(() => shareCall?.path ?? "")
      .toBe("/api/v1/instances/g1/media");
    expect((shareCall as unknown as Captured).body).toEqual({ mediaId: "m1" });

    await expect(page.getByText("写真を共有しました。")).toBeVisible();
    await expect(page.locator('img[src^="blob:"]')).toHaveCount(0);
    expect(
      page.getByRole("link", { name: "グループを見る" }),
    ).toHaveAttribute("href", "/group/?id=g1");
  });

  test("rejects an image over 15MB client-side without calling the API", async ({
    page,
  }) => {
    let apiCalls = 0;
    await mockApi(page, async (route) => {
      apiCalls += 1;
      await fulfillJson(route, 201, { id: "m1", width: 1, height: 1 });
    });

    await page.goto("/group/photo/?id=g1");
    await page.setInputFiles('[data-testid="space-photo-input"]', {
      name: "huge.jpg",
      mimeType: "image/jpeg",
      buffer: Buffer.alloc(MAX_MEDIA_BYTES + 1),
    });

    await expect(page.getByText("画像は 15MB までです")).toBeVisible();
    await expect(page.locator('img[src^="blob:"]')).toHaveCount(0);
    expect(apiCalls).toBe(0);
  });

  test("rejects a non-image file client-side without calling the API", async ({
    page,
  }) => {
    let apiCalls = 0;
    await mockApi(page, async (route) => {
      apiCalls += 1;
      await fulfillJson(route, 201, { id: "m1", width: 1, height: 1 });
    });

    await page.goto("/group/photo/?id=g1");
    await page.setInputFiles('[data-testid="space-photo-input"]', {
      name: "note.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("hello"),
    });

    await expect(
      page.getByText("JPEG・PNG・WebP の画像を選んでください"),
    ).toBeVisible();
    expect(apiCalls).toBe(0);
  });

  test("shows the reason when the share is forbidden (403)", async ({ page }) => {
    await mockApi(page, async (route, request) => {
      const pathname = apiPath(request.url());
      if (request.method() === "POST" && pathname === "/api/v1/media") {
        await fulfillJson(route, 201, { id: "m1", width: 1, height: 1 });
        return;
      }
      if (pathname === "/api/v1/instances/g1/media") {
        await fulfillJson(route, 403, {
          error: { code: "forbidden", message: "forbidden" },
        });
        return;
      }
      await fulfillJson(route, 404, NOT_FOUND);
    });

    await page.goto("/group/photo/?id=g1");
    await page.setInputFiles('[data-testid="space-photo-input"]', {
      name: "photo.png",
      mimeType: "image/png",
      buffer: PNG_BYTES,
    });
    await page.getByRole("button", { name: "共有する" }).click();

    await expect(
      page.getByText("この Space に写真を共有する権限がありません"),
    ).toBeVisible();
  });

  test("shows the preparation notice when the share endpoint is unavailable (404)", async ({
    page,
  }) => {
    await mockApi(page, async (route, request) => {
      const pathname = apiPath(request.url());
      if (request.method() === "POST" && pathname === "/api/v1/media") {
        await fulfillJson(route, 201, { id: "m1", width: 1, height: 1 });
        return;
      }
      await fulfillJson(route, 404, NOT_FOUND);
    });

    await page.goto("/group/photo/?id=g1");
    await page.setInputFiles('[data-testid="space-photo-input"]', {
      name: "photo.png",
      mimeType: "image/png",
      buffer: PNG_BYTES,
    });
    await page.getByRole("button", { name: "共有する" }).click();

    await expect(page.getByText("写真の共有は準備中です")).toBeVisible();
  });
});
