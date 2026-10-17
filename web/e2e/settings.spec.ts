import { mkdirSync } from "node:fs";
import path from "node:path";
import { expect, test, type Page } from "@playwright/test";

const SESSION_KEY = "chain.auth.session.v1";

type StoredSession = {
  did: string;
  accessToken: string;
  refreshToken: string;
  accessExpiresAtMs: number;
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

function makeStoredSession(expiresInMs = 60 * 60 * 1000): StoredSession {
  const did = "did:plc:testuser";
  const now = Math.floor(Date.now() / 1000);
  const accessToken = fakeJwt({ sub: did, iat: now, exp: now + 900 });
  return {
    did,
    accessToken,
    refreshToken: "refresh-token-0",
    accessExpiresAtMs: Date.now() + expiresInMs,
  };
}

async function useStoredSession(
  page: Page,
  session: StoredSession,
): Promise<void> {
  await page.addInitScript(
    ({ key, value }) => {
      window.localStorage.setItem(key, value);
    },
    { key: SESSION_KEY, value: JSON.stringify(session) },
  );
}

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-8-chain-fe-be-gap-closure.png",
);

test.describe("settings reveal delay", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("shows 3 modes and only the 4 backend presets", async ({ page }) => {
    await page.goto("/settings/");

    const reveal = page.locator("section", { hasText: "すれ違いの公開" });
    const modes = reveal.getByRole("group", { name: "相手に見えるまで" });

    await expect(modes.getByRole("switch")).toHaveCount(3);
    await expect(modes.getByRole("switch", { name: "すぐ" })).toBeVisible();
    await expect(modes.getByRole("switch", { name: "一定時間後" })).toBeVisible();
    await expect(modes.getByRole("switch", { name: "非公開" })).toBeVisible();

    const presets = reveal.getByRole("group", { name: "公開までの時間" });
    await expect(presets.getByRole("radio")).toHaveCount(4);
    await expect(presets.getByRole("radio", { name: "1時間" })).toBeVisible();
    await expect(presets.getByRole("radio", { name: "6時間" })).toBeVisible();
    await expect(presets.getByRole("radio", { name: "1日" })).toBeVisible();
    await expect(presets.getByRole("radio", { name: "3日" })).toBeVisible();

    await expect(presets.getByRole("radio", { name: "3時間" })).toHaveCount(0);
    await expect(presets.getByRole("radio", { name: "12時間" })).toHaveCount(0);
  });

  test("choosing 非公開 selects private and hides the presets", async ({
    page,
  }) => {
    await page.goto("/settings/");

    const reveal = page.locator("section", { hasText: "すれ違いの公開" });
    const modes = reveal.getByRole("group", { name: "相手に見えるまで" });

    await modes.getByRole("switch", { name: "非公開" }).click();

    await expect(
      modes.getByRole("switch", { name: "非公開" }),
    ).toHaveAttribute("aria-checked", "true");
    await expect(modes.getByRole("switch", { name: "すぐ" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    await expect(
      modes.getByRole("switch", { name: "一定時間後" }),
    ).toHaveAttribute("aria-checked", "false");
    await expect(reveal.getByRole("status")).toHaveText("非公開");
    await expect(
      reveal.getByRole("group", { name: "公開までの時間" }),
    ).toHaveCount(0);
  });

  test("keyboard selection works and presets update the value", async (
    { page },
    testInfo,
  ) => {
    await page.goto("/settings/");

    const reveal = page.locator("section", { hasText: "すれ違いの公開" });
    const modes = reveal.getByRole("group", { name: "相手に見えるまで" });

    const immediate = modes.getByRole("switch", { name: "すぐ" });
    await immediate.focus();
    await page.keyboard.press("Enter");
    await expect(immediate).toHaveAttribute("aria-checked", "true");
    await expect(reveal.getByRole("status")).toHaveText("すぐ");
    await expect(
      reveal.getByRole("group", { name: "公開までの時間" }),
    ).toHaveCount(0);

    await modes.getByRole("switch", { name: "一定時間後" }).click();
    const presets = reveal.getByRole("group", { name: "公開までの時間" });
    await expect(presets.getByRole("radio")).toHaveCount(4);
    await presets.getByRole("radio", { name: "3日" }).click();
    await expect(presets.getByRole("radio", { name: "3日" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    await expect(reveal.getByRole("status")).toHaveText("3日");

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH, fullPage: true });
    }
  });
});
