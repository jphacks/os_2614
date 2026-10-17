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

/** GET /me の応答 (controller.meResponse)。composer のアバター表示に使う。 */
const ME = {
  did: "did:plc:testuser",
  handle: "testuser.bsky.social",
  displayName: "じぶん",
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

const ORIGINAL = post("p1", "けんた", "元の投稿です。", "2026-10-17T02:30:00.000Z");
const OTHER = post("p2", "みさき", "一覧で返ってくる投稿です。", "2026-10-17T01:12:00.000Z");

const PAGE1 = { items: [ORIGINAL, OTHER] };

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-17-chain-fe-be-gap-closure.png",
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

test.describe("group reply sheet", () => {
  test("shows 準備中 instead of a fake reply submit and renders the API list", async ({
    page,
  }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    const postedToPosts: string[] = [];
    page.on("request", (request) => {
      if (request.method() === "POST" && request.url().includes("/api/v1/posts")) {
        postedToPosts.push(request.url());
      }
    });

    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP);
        return;
      }
      if (url.pathname === "/api/v1/instances/g1/posts") {
        await fulfillJson(route, 200, PAGE1);
        return;
      }
      if (url.pathname === "/api/v1/me") {
        await fulfillJson(route, 200, ME);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/replies/?id=g1&post=p1");

    const dialog = page.getByRole("dialog", { name: "返信" });
    await expect(dialog).toBeVisible();

    // 元の投稿と、API が返した一覧をそのまま描画する (スレッド構造は作らない)。
    await expect(dialog.getByText(ORIGINAL.text)).toBeVisible();
    await expect(dialog.getByText(OTHER.text)).toBeVisible();

    // 送信は準備中: 通知が見え、送信ボタンと入力は無効。
    await expect(dialog.getByText("返信は準備中です")).toBeVisible();
    await expect(dialog.getByRole("button", { name: "返信を送信" })).toBeDisabled();
    await expect(dialog.getByLabel("返信を書く")).toBeDisabled();

    // 無言の no-op ではない: 操作しても POST /posts は飛ばない。
    expect(postedToPosts).toHaveLength(0);

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }
  });

  test("shows an inline error when the posts API fails", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP);
        return;
      }
      if (url.pathname === "/api/v1/me") {
        await fulfillJson(route, 200, ME);
        return;
      }
      await fulfillJson(route, 500, {
        error: { code: "internal", message: "boom" },
      });
    });

    await page.goto("/group/replies/?id=g1&post=p1");

    await expect(page.getByText("投稿を読み込めませんでした")).toBeVisible();
    await expect(page.getByRole("button", { name: "再読み込み" })).toBeVisible();
    await expect(page.getByRole("dialog", { name: "返信" })).toHaveCount(0);
  });
});
