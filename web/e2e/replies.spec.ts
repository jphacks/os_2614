import { expect, test, type Page, type Request, type Route } from "@playwright/test";

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

type ReplyRef = { root: string; parent: string };

/** GET /instances/{spaceId}/posts の 1 件 (service.FeedItem、10 §4.2)。 */
function post(
  uri: string,
  displayName: string,
  text: string,
  createdAt: string,
  reply?: ReplyRef,
) {
  return {
    kind: "post",
    uri,
    spaceId: "g1",
    author: { did: `did:plc:${uri}`, handle: `${uri}.bsky.social`, displayName },
    text,
    format: "plain",
    media: [],
    createdAt,
    ...(reply ? { reply } : {}),
  };
}

const ORIGINAL = post("p1", "けんた", "元の投稿です。", "2026-10-17T02:30:00.000Z");
const OTHER = post("p2", "みさき", "別スレッドの投稿です。", "2026-10-17T01:12:00.000Z");
const SENT = post("p3", "じぶん", "ありがとう！", "2026-10-17T03:00:00.000Z", {
  root: "p1",
  parent: "p1",
});
/** 返信への返信を開くケース。root はスレッド先頭 (p1)、parent はこの投稿。 */
const NESTED_OPENED = post(
  "p2b",
  "みさき",
  "返信への返信を開きます。",
  "2026-10-17T02:40:00.000Z",
  { root: "p1", parent: "p1" },
);

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

/** JWT 風の文字列。session.ts は payload の iat / exp だけを読む。 */
function jwtFor(did: string): string {
  const now = Math.floor(Date.now() / 1000);
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify({ sub: did, iat: now, exp: now + 900 }));
  return `${header}.${payload}.signature`;
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

function apiPath(url: string): string {
  return decodeURIComponent(new URL(url).pathname);
}

type ReplyCapture = { path: string; body: unknown };

/**
 * group / 返信画面の共通 mock。POST /posts の body を captured に集め、
 * 送信後の一覧 (afterPost) を返せるようにする。
 */
async function mockReplies(
  page: Page,
  opts: {
    items: unknown[];
    afterPost?: unknown[];
    replyStatus?: number;
    replyBody?: unknown;
    instanceState?: string;
    canWrite?: boolean;
    captured: ReplyCapture[];
  },
): Promise<void> {
  let posted = false;
  await mockApi(page, async (route, request) => {
    const method = request.method();
    const pathname = apiPath(request.url());
    if (method === "GET" && pathname === "/api/v1/groups/g1") {
      await fulfillJson(route, 200, GROUP);
      return;
    }
    if (method === "GET" && pathname === "/api/v1/me") {
      await fulfillJson(route, 200, ME);
      return;
    }
    if (method === "GET" && pathname === "/api/v1/instances") {
      await fulfillJson(route, 200, {
        items: [{
          spaceId: "g1",
          type: "group",
          name: GROUP.name,
          state: opts.instanceState ?? "active",
          memberCount: 8,
          canWrite: opts.canWrite ?? true,
        }],
      });
      return;
    }
    if (method === "GET" && pathname === "/api/v1/instances/g1/thread") {
      const source = posted && opts.afterPost ? opts.afterPost : opts.items;
      const requested = new URL(request.url()).searchParams.get("root");
      const opened = source.find((item) => (item as { uri?: string }).uri === requested);
      const root = (opened as { reply?: ReplyRef } | undefined)?.reply?.root ?? requested;
      await fulfillJson(route, 200, {
        root: source.find((item) => (item as { uri?: string }).uri === root) ?? ORIGINAL,
        replies: source.filter((item) => (item as { reply?: ReplyRef }).reply?.root === root),
      });
      return;
    }
    if (method === "GET" && pathname === "/api/v1/instances/g1/posts") {
      await fulfillJson(route, 200, {
        items: posted && opts.afterPost ? opts.afterPost : opts.items,
      });
      return;
    }
    if (method === "POST" && pathname === "/api/v1/posts") {
      opts.captured.push({ path: pathname, body: request.postDataJSON() });
      if (opts.replyStatus !== undefined && opts.replyStatus >= 300) {
        await fulfillJson(route, opts.replyStatus, opts.replyBody);
        return;
      }
      posted = true;
      await fulfillJson(route, 201, { items: [{ uri: "p3", spaceId: "g1" }] });
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });
}

test.describe("group reply thread", () => {
  test("sends a reply with reply.root/parent and shows it in the thread", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());
    const captured: ReplyCapture[] = [];
    await mockReplies(page, {
      items: [ORIGINAL, OTHER],
      afterPost: [SENT, ORIGINAL, OTHER],
      captured,
    });

    await page.goto("/group/replies/?id=g1&post=p1");

    const dialog = page.getByRole("dialog", { name: "返信" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(ORIGINAL.text)).toBeVisible();
    // reply を持たない別スレッドの投稿は返信として混ざらない。
    await expect(dialog.getByText(OTHER.text)).toHaveCount(0);

    const input = dialog.getByLabel("返信を書く");
    await input.fill("ありがとう！");
    await dialog.getByRole("button", { name: "返信を送信" }).click();

    await expect(dialog.getByText(SENT.text)).toBeVisible();
    await expect(input).toHaveValue("");

    expect(captured).toHaveLength(1);
    expect(captured[0].path).toBe("/api/v1/posts");
    expect(captured[0].body).toEqual({
      spaceIds: ["g1"],
      text: "ありがとう！",
      format: "plain",
      reply: { root: "p1", parent: "p1" },
    });
  });

  test("replies to a reply with the thread root as reply.root", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    const captured: ReplyCapture[] = [];
    await mockReplies(page, { items: [ORIGINAL, NESTED_OPENED], captured });

    await page.goto("/group/replies/?id=g1&post=p2b");

    const dialog = page.getByRole("dialog", { name: "返信" });
    await expect(dialog.getByText(NESTED_OPENED.text)).toBeVisible();
    // 同じ root を持つスレッド先頭の投稿は返信一覧に並ぶ。
    await expect(dialog.getByText(ORIGINAL.text)).toBeVisible();

    await dialog.getByLabel("返信を書く").fill("了解です");
    await dialog.getByRole("button", { name: "返信を送信" }).click();

    await expect.poll(() => captured.length).toBe(1);
    expect(captured[0].body).toEqual({
      spaceIds: ["g1"],
      text: "了解です",
      format: "plain",
      reply: { root: "p1", parent: "p2b" },
    });
  });

  test("shows an inline error and keeps the text when the reply is rejected", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());
    const captured: ReplyCapture[] = [];
    await mockReplies(page, {
      items: [ORIGINAL],
      captured,
      replyStatus: 410,
      replyBody: { error: { code: "gone", message: "期限切れか、終了しています" } },
    });

    await page.goto("/group/replies/?id=g1&post=p1");

    const dialog = page.getByRole("dialog", { name: "返信" });
    const input = dialog.getByLabel("返信を書く");
    await input.fill("送れない返信");
    await dialog.getByRole("button", { name: "返信を送信" }).click();

    await expect(
      dialog.getByText("この投稿は終了しているため返信できません。"),
    ).toBeVisible();
    // 失敗した本文は消さない (そのまま再送できる)。
    await expect(input).toHaveValue("送れない返信");
    expect(captured).toHaveLength(1);
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
    await expect(page.getByRole("dialog", { name: "返信" })).toBeVisible();
    await expect(page.getByRole("dialog", { name: "返信" }).getByText("返信を読み込めませんでした")).toBeVisible();
    await expect(page.getByRole("dialog", { name: "返信" }).getByRole("button", { name: "再試行" })).toBeVisible();
  });

  for (const state of ["tentative", "archived", "left"]) {
    test(`hides the composer for a ${state} space`, async ({ page }) => {
      await useStoredSession(page, makeStoredSession());
      const captured: ReplyCapture[] = [];
      await mockReplies(page, {
        items: [ORIGINAL],
        captured,
        instanceState: state,
        canWrite: false,
      });

      await page.goto("/group/replies/?id=g1&post=p1");

      const dialog = page.getByRole("dialog", { name: "返信" });
      await expect(dialog).toBeVisible();
      await expect(dialog.getByLabel("返信を書く")).toHaveCount(0);
    });
  }
});
