import {
  expect,
  test,
  type Page,
  type Route,
} from "@playwright/test";

import {
  parseMarkdown,
  type Inline,
  type MarkdownBlock,
} from "../src/data/markdown";

// 純関数のテスト。ブラウザもサーバーも使わないので決定的に速く回る。
// 生 HTML を解釈しないこと (spec 10 §3:36) を最優先で固定する。

const SAFE_BLOCK_TYPES = new Set<MarkdownBlock["type"]>([
  "heading",
  "paragraph",
  "code",
  "blockquote",
  "list",
  "table",
  "hr",
  "mathBlock",
]);

function inlineText(nodes: Inline[]): string {
  return nodes
    .map((node) => {
      switch (node.type) {
        case "text":
        case "code":
          return node.value;
        case "math":
          return node.tex;
        case "strong":
        case "em":
        case "strike":
        case "link":
          return inlineText(node.children);
      }
    })
    .join("");
}

function collectInlines(blocks: MarkdownBlock[]): Inline[] {
  const result: Inline[] = [];
  for (const block of blocks) {
    switch (block.type) {
      case "heading":
      case "paragraph":
        result.push(...block.children);
        break;
      case "blockquote":
        result.push(...collectInlines(block.children));
        break;
      case "list":
        for (const item of block.items) {
          result.push(...item);
        }
        break;
      case "table":
        for (const cell of block.header) {
          result.push(...cell);
        }
        for (const row of block.rows) {
          for (const cell of row) {
            result.push(...cell);
          }
        }
        break;
      case "code":
      case "hr":
      case "mathBlock":
        break;
    }
  }
  return result;
}

test.describe("parseMarkdown", () => {
  test("returns [] for empty and whitespace-only input", () => {
    expect(parseMarkdown("")).toEqual([]);
    expect(parseMarkdown("   \n ")).toEqual([]);
    expect(parseMarkdown("\n\n\t\n")).toEqual([]);
  });

  test("parses a GFM table into header cells and rows", () => {
    const blocks = parseMarkdown(
      [
        "| 名前 | 好きなもの |",
        "| --- | :--: |",
        "| ゆい | 写真 |",
        "| そうた | 珈琲 |",
      ].join("\n"),
    );

    expect(blocks).toHaveLength(1);
    const table = blocks[0];
    expect(table?.type).toBe("table");
    if (table?.type !== "table") {
      throw new Error("expected a table block");
    }

    expect(table.header.map(inlineText)).toEqual(["名前", "好きなもの"]);
    expect(table.rows).toHaveLength(2);
    expect(table.rows[0]?.map(inlineText)).toEqual(["ゆい", "写真"]);
    expect(table.rows[1]?.map(inlineText)).toEqual(["そうた", "珈琲"]);
  });

  test("extracts inline math from a sentence", () => {
    const blocks = parseMarkdown("面積は $x^2$ で求まる。");

    const paragraph = blocks[0];
    expect(paragraph?.type).toBe("paragraph");
    if (paragraph?.type !== "paragraph") {
      throw new Error("expected a paragraph block");
    }

    expect(paragraph.children).toContainEqual({ type: "math", tex: "x^2" });
    expect(inlineText(paragraph.children)).toBe("面積は x^2 で求まる。");
  });

  test("does not re-parse markers inside code spans or math", () => {
    const blocks = parseMarkdown("`**not bold**` と $a*b*c$");

    const paragraph = blocks[0];
    if (paragraph?.type !== "paragraph") {
      throw new Error("expected a paragraph block");
    }

    expect(paragraph.children).toContainEqual({
      type: "code",
      value: "**not bold**",
    });
    expect(paragraph.children).toContainEqual({ type: "math", tex: "a*b*c" });
  });

  test("parses block math on its own lines", () => {
    expect(parseMarkdown("$$\nE=mc^2\n$$")).toEqual([
      { type: "mathBlock", tex: "E=mc^2" },
    ]);
  });

  test("parses single-line block math", () => {
    expect(parseMarkdown("$$E=mc^2$$")).toEqual([
      { type: "mathBlock", tex: "E=mc^2" },
    ]);
  });

  test("never interprets raw HTML", () => {
    const blocks = parseMarkdown(
      '<script>alert(1)</script>\n\n<img src="x" onerror="alert(2)">',
    );

    const blockTypes = blocks.map((block) => block.type);
    expect(blockTypes).not.toContain("html");
    expect(blockTypes.every((type) => SAFE_BLOCK_TYPES.has(type))).toBe(true);
    expect(blockTypes).toEqual(["paragraph", "paragraph"]);

    const first = blocks[0];
    expect(first?.type).toBe("paragraph");
    if (first?.type !== "paragraph") {
      throw new Error("expected a paragraph block");
    }

    // `<script>` は素のテキストとしてのみ存在し、inline も text だけになる。
    expect(first.children.every((inline) => inline.type === "text")).toBe(true);
    expect(inlineText(first.children)).toContain("alert(1)");
    expect(inlineText(first.children)).toBe("<script>alert(1)</script>");

    // どこにも anchor/link Inline は生成されない。
    const inlines = collectInlines(blocks);
    expect(inlines.some((inline) => inline.type === "link")).toBe(false);
  });

  test("never throws on unbalanced or malformed markers", () => {
    const inputs = [
      "**bold",
      "[link](",
      "$$",
      "`unclosed",
      "~~strike",
      "| a | b |\n| --- |",
      "> ",
      "#",
      "- ",
      "\\",
      "***",
      "$$$",
    ];

    for (const input of inputs) {
      expect(() => parseMarkdown(input)).not.toThrow();
      expect(Array.isArray(parseMarkdown(input))).toBe(true);
    }
  });
});

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";

const CORS_HEADERS: Record<string, string> = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,OPTIONS",
  "access-control-allow-headers": "authorization,content-type",
  "access-control-max-age": "600",
};

const GROUP = {
  id: "g1",
  name: "情報工学科 3年",
  description: "",
  visibility: "open",
  memberCount: 8,
  isMember: true,
  state: "active",
  radiusM: 40,
};

const RAW_IMG = '<img src="x" onerror="window.__xss=1">';
const RAW_SCRIPT = "<script>window.__xss=1</script>";

const MARKDOWN_BODY = [
  "| 名前 | 好きなもの |",
  "| --- | --- |",
  "| ゆい | 写真 |",
  "",
  "面積は $x^2$ です。",
  "",
  RAW_IMG,
  "",
  RAW_SCRIPT,
].join("\n");

const POSTS = {
  items: [
    {
      kind: "post",
      uri: "at://did:plc:kenta/jp.i32.chain.instance.post/1",
      spaceId: "g1",
      author: {
        did: "did:plc:kenta",
        handle: "kenta.bsky.social",
        displayName: "けんた",
      },
      text: MARKDOWN_BODY,
      format: "markdown",
      media: [],
      createdAt: "2026-10-17T02:30:00.000Z",
    },
  ],
};

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

async function useStoredSession(page: Page): Promise<void> {
  const did = "did:plc:testuser";
  const now = Math.floor(Date.now() / 1000);
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify({ sub: did, iat: now, exp: now + 900 }));
  const session = {
    did,
    accessToken: `${header}.${payload}.signature`,
    refreshToken: "refresh-token-0",
    accessExpiresAtMs: Date.now() + 60 * 60 * 1000,
  };
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

test.describe("MarkdownBody rendering", () => {
  test("renders tables and math and neutralizes raw HTML (10 §3:36)", async ({
    page,
  }) => {
    await useStoredSession(page);

    await page.route(`${API_BASE}/**`, async (route) => {
      const request = route.request();
      if (request.method() === "OPTIONS") {
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      const pathname = new URL(request.url()).pathname;
      if (pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP);
        return;
      }
      if (pathname === "/api/v1/instances/g1/posts") {
        await fulfillJson(route, 200, POSTS);
        return;
      }
      if (pathname === "/api/v1/instances/g1/pins") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/?id=g1");

    // 表が本物の table として描画される。
    await expect(page.getByRole("columnheader", { name: "名前" })).toBeVisible();
    await expect(page.getByRole("cell", { name: "写真" })).toBeVisible();
    // 数式はコードとして描画される。
    await expect(page.getByText("x^2")).toBeVisible();
    // 生 HTML は要素にならず、テキストとしてのみ存在する。
    await expect(page.getByText(RAW_IMG)).toBeVisible();
    await expect(page.getByText(RAW_SCRIPT)).toBeVisible();
    await expect(page.locator("img[onerror]")).toHaveCount(0);

    const injected = await page.evaluate(() => {
      const scripts = Array.from(document.querySelectorAll("script"));
      return scripts.filter((node) => (node.textContent ?? "").includes("__xss"))
        .length;
    });
    expect(injected).toBe(0);

    const xss = await page.evaluate(
      () => (window as unknown as { __xss?: number }).__xss ?? 0,
    );
    expect(xss).toBe(0);
  });
});
