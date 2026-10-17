import fs from "node:fs";
import { fileURLToPath } from "node:url";
import puppeteer from "puppeteer-core";

const cdpUrl = process.env.PW_CDP_URL ?? "http://127.0.0.1:9222";
const artifactDir = fileURLToPath(new URL("../artifacts/", import.meta.url)).replace(/\/$/, "");
fs.mkdirSync(artifactDir, { recursive: true });

const version = await fetch(`${cdpUrl}/json/version`).then((response) => {
  if (!response.ok) {
    throw new Error(`CDP に接続できません: ${cdpUrl}`);
  }
  return response.json();
});

const browser = await puppeteer.connect({
  browserWSEndpoint: version.webSocketDebuggerUrl,
  defaultViewport: null,
});

try {
  const pages = await browser.pages();
  const page =
    pages.find((candidate) => candidate.url().includes("localhost")) ?? pages[0];
  if (!page) {
    throw new Error("WebView のページが見つかりません");
  }

  await page.waitForFunction(
    (text) => document.body.innerText.includes(text),
    { timeout: 30_000 },
    "ちかく",
  );

  const nav = await page.$('nav[aria-label="メインナビゲーション"]');
  if (!nav) {
    throw new Error("メインナビゲーションが見つかりません");
  }

  const friendsLink = await page.$(
    'nav[aria-label="メインナビゲーション"] a[href="/friends/"]',
  );
  if (!friendsLink) {
    throw new Error("友達タブのリンクが見つかりません");
  }

  await Promise.all([
    page.waitForFunction(
      () => document.body.innerText.includes("位置はエリア名だけ共有しています"),
      { timeout: 15_000 },
    ),
    friendsLink.click(),
  ]);

  if (!page.url().includes("/friends")) {
    throw new Error(`画面遷移に失敗しました: ${page.url()}`);
  }

  await page.screenshot({
    path: `${artifactDir}/android-webview.png`,
    fullPage: false,
  });

  console.log(`ANDROID WEBVIEW OK: ${page.url()}`);
  console.log(`screenshot: ${artifactDir}/android-webview.png`);
} finally {
  await browser.disconnect();
}
