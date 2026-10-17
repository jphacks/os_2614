import { mkdirSync } from "node:fs";
import path from "node:path";
import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
} from "@playwright/test";

import {
  MAX_MEDIA_BYTES,
  validateMediaFile,
} from "../src/components/feature/compose/composeInput";

// Compose の画像添付 (10 §4.4)。選んだファイルは multipart で POST /media へ送り、
// 201 {id,width,height} の id を POST /posts の media {id,alt} に載せる。プレビューは
// object URL。JPEG/PNG/WebP 以外と 15MB 超はアップロード前にクライアントで拒否する。

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";

const CORS_HEADERS: Record<string, string> = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,OPTIONS",
  "access-control-allow-headers": "authorization,content-type",
  "access-control-max-age": "600",
};

/** GET /me (controller.meResponse)。著者アバター用。 */
const ME = {
  did: "did:plc:testuser",
  handle: "test.bsky.social",
  displayName: "てすとたろう",
  description: "",
  settings: {
    encounterMode: "delayed",
    encounterDelaySec: 3600,
    friendBeaconShare: true,
    partyAuto: true,
    locationPaused: false,
    warnDisplay: "blur",
    contentScanConsentVersion: 1,
    notify: {},
  },
};

/** GET /instances (controller.instanceItem、13 §2) の 1 件。 */
const GROUP_INSTANCE = {
  spaceId: "g1",
  type: "group",
  name: "情報科学演習 A",
  state: "active",
  memberCount: 3,
  canWrite: true,
};

/** 64x64 の単色 PNG。プレビュー (object URL) が実画像として描画されることを確認する。 */
const PNG_BYTES = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAIAAAAlC+aJAAAAT0lEQVR42u3PQQkAAAgEsCtjMuNbwgi+hcEKLNXzWgQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQELguYZjFanhwbHAAAAABJRU5ErkJggg==",
  "base64",
);

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-15-chain-fe-be-gap-closure.png",
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

/** POST /posts の body (10 §4.1)。テストで形を検査する。 */
type PostBody = {
  spaceIds: string[];
  text: string;
  format: string;
  media: { id: string; alt: string }[];
  alsoPublic: boolean;
};

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

/** GET /instances と GET /me を返し、それ以外は 404 にする共通の土台。 */
function baseMock(): ApiMock {
  return async (route, request) => {
    const url = new URL(request.url());
    if (url.pathname === "/api/v1/instances") {
      await fulfillJson(route, 200, { items: [GROUP_INSTANCE] });
      return;
    }
    if (url.pathname === "/api/v1/me") {
      await fulfillJson(route, 200, ME);
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  };
}

/** /compose/ を開き、送信先の読み込みを待つ。 */
async function openCompose(page: Page) {
  await page.goto("/compose/");
  await expect(page.getByLabel("投稿本文")).toBeVisible({ timeout: 15_000 });
}

test.describe("compose media", () => {
  test("uploads the picked image via multipart POST /media and sends its id in POST /posts media", async ({
    page,
  }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    let mediaContentType: string | null = null;
    let mediaBody = "";
    let mediaCalls = 0;
    let postBody: PostBody | null = null;

    const base = baseMock();
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/media" && request.method() === "POST") {
        mediaCalls += 1;
        mediaContentType = request.headers()["content-type"] ?? null;
        mediaBody = request.postDataBuffer()?.toString("utf8") ?? "";
        await fulfillJson(route, 201, { id: "m1", width: 800, height: 600 });
        return;
      }
      if (url.pathname === "/api/v1/posts" && request.method() === "POST") {
        postBody = request.postDataJSON() as PostBody;
        await fulfillJson(route, 201, {
          items: [{ uri: "at://did:plc:testuser/jp.i32.chain.instance/g1", spaceId: "g1" }],
        });
        return;
      }
      await base(route, request);
    });

    await openCompose(page);
    await page.setInputFiles('[data-testid="compose-media-input"]', {
      name: "photo.png",
      mimeType: "image/png",
      buffer: PNG_BYTES,
    });

    // multipart で file フィールドにファイル名が載る（署名付き URL は返らないので preview は object URL）。
    await expect.poll(() => mediaContentType ?? "").toContain("multipart/form-data");
    expect(mediaCalls).toBe(1);
    expect(mediaBody).toContain('name="file"');
    expect(mediaBody).toContain('filename="photo.png"');
    await expect(page.locator('img[src^="blob:"]')).toBeVisible();

    await page.getByLabel("投稿本文").fill("写真つき投稿");

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH, fullPage: true });
    }

    await page.getByRole("button", { name: "投稿する" }).click();
    await page.waitForURL(/\/group\/\?id=g1/);

    expect(postBody).not.toBeNull();
    expect((postBody as unknown as PostBody).media).toEqual([{ id: "m1", alt: "" }]);
  });

  test("rejects a non-image file client-side without calling POST /media", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let mediaCalls = 0;
    const base = baseMock();
    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/media") {
        mediaCalls += 1;
        await fulfillJson(route, 201, { id: "m1", width: 1, height: 1 });
        return;
      }
      await base(route, request);
    });

    await openCompose(page);
    await page.setInputFiles('[data-testid="compose-media-input"]', {
      name: "note.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("hello"),
    });

    await expect(
      page.getByText("JPEG・PNG・WebP の画像を選んでください"),
    ).toBeVisible();
    await expect(page.locator('img[src^="blob:"]')).toHaveCount(0);
    expect(mediaCalls).toBe(0);
  });

  test("rejects an image over 15MB client-side without calling POST /media", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let mediaCalls = 0;
    const base = baseMock();
    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/media") {
        mediaCalls += 1;
        await fulfillJson(route, 201, { id: "m1", width: 1, height: 1 });
        return;
      }
      await base(route, request);
    });

    await openCompose(page);
    await page.setInputFiles('[data-testid="compose-media-input"]', {
      name: "huge.jpg",
      mimeType: "image/jpeg",
      buffer: Buffer.alloc(MAX_MEDIA_BYTES + 1),
    });

    await expect(page.getByText("画像は 15MB までです")).toBeVisible();
    await expect(page.locator('img[src^="blob:"]')).toHaveCount(0);
    expect(mediaCalls).toBe(0);
  });
});

test.describe("media file rules (純関数)", () => {
  test("accepts JPEG / PNG / WebP at the size limit", () => {
    for (const type of ["image/jpeg", "image/png", "image/webp"]) {
      expect(validateMediaFile({ type, size: MAX_MEDIA_BYTES })).toEqual({ ok: true });
    }
  });

  test("rejects other MIME types", () => {
    const result = validateMediaFile({ type: "image/gif", size: 1024 });
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toBe("JPEG・PNG・WebP の画像を選んでください");
    }
  });

  test("rejects files over 15MB", () => {
    const result = validateMediaFile({ type: "image/jpeg", size: MAX_MEDIA_BYTES + 1 });
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toBe("画像は 15MB までです");
    }
  });
});
