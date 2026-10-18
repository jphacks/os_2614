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
  buildCreatePostBody,
  hasPendingAttachments,
  validateComposeInput,
} from "../src/components/feature/compose/composeInput";

// POST /posts (10 §4.1) の送信配線。ブラウザ側は GET /instances で送信先を取り、
// 選んだ spaceIds と本文を spec 形の body で送る。画像は 1 Space のときだけ許可、
// 403 consent_required は同意導線、alsoPublic 非対応は正直に表示する。

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
type InstanceItem = {
  spaceId: string;
  type: string;
  name: string;
  state: string;
  memberCount: number;
  canWrite: boolean;
};

const GROUP_INSTANCE: InstanceItem = {
  spaceId: "g1",
  type: "group",
  name: "情報科学演習 A",
  state: "active",
  memberCount: 3,
  canWrite: true,
};

const PARTY_INSTANCE: InstanceItem = {
  spaceId: "p1",
  type: "party",
  name: "パーティ",
  state: "active",
  memberCount: 4,
  canWrite: true,
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-14-chain-fe-be-gap-closure.png",
);

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

/** /instances と /me を返し、それ以外は 404 にする共通の土台。 */
function baseMock(instances: InstanceItem[]): ApiMock {
  return async (route, request) => {
    const url = new URL(request.url());
    if (url.pathname === "/api/v1/instances") {
      await fulfillJson(route, 200, { items: instances });
      return;
    }
    if (url.pathname === "/api/v1/me") {
      await fulfillJson(route, 200, ME);
      return;
    }
    if (url.pathname === "/api/v1/groups/g1") {
      await fulfillJson(route, 200, {
        id: "g1",
        name: GROUP_INSTANCE.name,
        description: "",
        visibility: "open",
        memberCount: 3,
        isMember: true,
        state: "active",
      });
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  };
}

/** /compose/ を開き、送信先の読み込みを待って本文を入れる。 */
async function readyToPost(page: Page, text: string) {
  await page.goto("/compose/");
  const textarea = page.getByLabel("投稿本文");
  await expect(textarea).toBeVisible({ timeout: 15_000 });
  await textarea.fill(text);
}

test.describe("compose", () => {
  test("does not show a canWrite:false instance as a target", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/instances") {
        await fulfillJson(route, 200, {
          items: [GROUP_INSTANCE, { ...PARTY_INSTANCE, canWrite: false }],
        });
        return;
      }
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });

    await page.goto("/compose/");
    const targetChips = page.getByText("送信先").locator("..");
    await expect(
      targetChips.getByRole("button", { name: GROUP_INSTANCE.name }),
    ).toBeVisible();
    await expect(
      targetChips.getByRole("button", { name: PARTY_INSTANCE.name }),
    ).toHaveCount(0);
  });

  test("sends the spec body to POST /posts and opens the group timeline", async ({
    page,
  }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    let body: PostBody | null = null;
    const base = baseMock([GROUP_INSTANCE]);
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/posts" && request.method() === "POST") {
        body = request.postDataJSON() as PostBody;
        await fulfillJson(route, 201, {
          items: [{ uri: "at://did:plc:testuser/jp.i32.chain.instance/g1", spaceId: "g1" }],
        });
        return;
      }
      await base(route, request);
    });

    await readyToPost(page, "こんにちは");

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH, fullPage: true });
    }

    await page.getByRole("button", { name: "投稿する" }).click();

    await page.waitForURL(/\/group\/\?id=g1/);
    expect(body).not.toBeNull();
    // body は 10 §4.1 の 5 キーだけ。送信先は 1 件、media は空、format は plain。
    expect(body).toEqual({
      spaceIds: ["g1"],
      text: "こんにちは",
      format: "plain",
      media: [],
      alsoPublic: false,
    });
  });

  test("posts to two spaces with both spaceIds and no media", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let body: PostBody | null = null;
    const base = baseMock([GROUP_INSTANCE, PARTY_INSTANCE]);
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/posts" && request.method() === "POST") {
        body = request.postDataJSON() as PostBody;
        await fulfillJson(route, 201, {
          items: [
            { uri: "at://x/g1", spaceId: "g1" },
            { uri: "at://x/p1", spaceId: "p1" },
          ],
        });
        return;
      }
      await base(route, request);
    });

    await readyToPost(page, "両方に投稿");
    // 2 つ目の送信先 (パーティ) を選ぶ。
    await page.getByRole("button", { name: "パーティ" }).click();
    await page.getByRole("button", { name: "投稿する" }).click();

    await page.waitForURL(/\/group\/\?id=g1/);
    expect(body).not.toBeNull();
    const sent = body as unknown as PostBody;
    expect(sent.spaceIds).toEqual(["g1", "p1"]);
    expect(sent.media).toEqual([]);
  });

  test("surfaces 403 consent_required with the consent entry point", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    const base = baseMock([GROUP_INSTANCE]);
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/posts" && request.method() === "POST") {
        await fulfillJson(route, 403, {
          error: { code: "consent_required", message: "内容チェックへの同意が必要です" },
        });
        return;
      }
      await base(route, request);
    });

    await readyToPost(page, "同意が必要な投稿");
    await page.getByRole("button", { name: "投稿する" }).click();

    await expect(
      page.getByText("投稿には内容チェックへの同意が必要です"),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "同意する" })).toBeVisible();
  });

  test("surfaces the unsupported alsoPublic as an honest message", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let body: PostBody | null = null;
    const base = baseMock([GROUP_INSTANCE]);
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/posts" && request.method() === "POST") {
        body = request.postDataJSON() as PostBody;
        // backend は alsoPublic をまだ受け付けない想定 → details.fields.alsoPublic。
        await fulfillJson(route, 400, {
          error: {
            code: "invalid_request",
            message: "alsoPublic is not supported",
            details: { fields: { alsoPublic: "not supported" } },
          },
        });
        return;
      }
      await base(route, request);
    });

    await readyToPost(page, "公開投稿つき");
    await page.getByRole("button", { name: "公開", exact: true }).click();
    await page.getByRole("button", { name: "投稿する" }).click();

    await expect(
      page.getByText(
        "公開投稿は現在利用できません。オフにして再試行してください。",
      ),
    ).toBeVisible();
    expect(body).not.toBeNull();
    expect((body as unknown as PostBody).alsoPublic).toBe(true);
  });

  test("delivers to the nearby audience and opens the home feed", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let body: { text: string } | null = null;
    const base = baseMock([GROUP_INSTANCE]);
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/nearby/posts" && request.method() === "POST") {
        body = request.postDataJSON() as { text: string };
        await fulfillJson(route, 201, {
          uri: "at://did:plc:testuser/jp.i32.chain.feed.nearby/1",
        });
        return;
      }
      await base(route, request);
    });

    await readyToPost(page, "近くの人へ");

    // 近くの人が選ばれると TargetSelector の pill 名も「近くの人」になるため、
    // button[aria-pressed] で chip だけに絞る (pill は aria-pressed を持たない)。
    const nearbyChip = page.locator("button[aria-pressed]", { hasText: "近くの人" });
    const publicChip = page.locator("button[aria-pressed]", { hasText: "公開" });
    await nearbyChip.click();

    await expect(nearbyChip).toHaveAttribute("aria-pressed", "true");
    await expect(publicChip).toHaveAttribute("aria-pressed", "false");

    await page.getByRole("button", { name: "投稿する" }).click();

    await page.waitForURL(/\/home\/?$/);
    expect(body).not.toBeNull();
    expect(body).toEqual({ text: "近くの人へ" });
  });
});

test.describe("compose input rules (純関数)", () => {
  test("refuses media when more than one space is selected", () => {
    // 10 §4.1: media は spaceIds が 1 件のときだけ。
    const result = validateComposeInput({
      spaceIds: ["g1", "p1"],
      text: "二つの Space に画像は付けられない",
      format: "plain",
      attachments: [{ key: "a", id: "m1", alt: "写真" }],
      alsoPublic: false,
      nearby: false,
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toBe("画像を付けられるのは送信先が 1 つのときだけです");
    }
  });

  test("allows media for exactly one space", () => {
    const result = validateComposeInput({
      spaceIds: ["g1"],
      text: "",
      format: "plain",
      attachments: [{ key: "a", id: "m1", alt: "" }],
      alsoPublic: false,
      nearby: false,
    });

    expect(result.ok).toBe(true);
  });

  test("builds the spec body and drops attachments that are not uploaded yet", () => {
    const body = buildCreatePostBody({
      spaceIds: ["g1"],
      text: "本文",
      format: "plain",
      attachments: [
        { key: "done", id: "m1", alt: "アップロード済み" },
        { key: "pending", alt: "未アップロード" },
      ],
      alsoPublic: true,
      nearby: false,
    });

    expect(body).toEqual({
      spaceIds: ["g1"],
      text: "本文",
      format: "plain",
      media: [{ id: "m1", alt: "アップロード済み" }],
      alsoPublic: true,
    });
    // 未アップロードが残っていることは呼び出し側が検知できる (黙って落とさない)。
    expect(hasPendingAttachments([
      { key: "done", id: "m1" },
      { key: "pending" },
    ])).toBe(true);
    expect(hasPendingAttachments([{ key: "done", id: "m1" }])).toBe(false);
  });

  test("requires text or media", () => {
    const result = validateComposeInput({
      spaceIds: ["g1"],
      text: "   ",
      format: "plain",
      attachments: [],
      alsoPublic: false,
      nearby: false,
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toBe("本文か画像が必要です");
    }
  });
});
