// node type.mjs <css selector> <text> [--clear]  : WebView の要素に 1 文字ずつ入力する
import puppeteer from "/Users/ringo/Documents/docs/projects/os_2614/web/node_modules/.pnpm/puppeteer-core@25.14.0_yauzl@2.10.0/node_modules/puppeteer-core/lib/puppeteer/puppeteer-core.js";
const [sel, text, flag] = process.argv.slice(2);
const b = await puppeteer.connect({ browserURL: "http://localhost:9222", defaultViewport: null });
const [p] = await b.pages();
await p.focus(sel);
if (flag === "--clear") {
  await p.evaluate((s) => { const e = document.querySelector(s); e.select(); }, sel);
  await p.keyboard.press("Backspace");
  await new Promise((r) => setTimeout(r, 300));
}
for (const ch of [...text]) {
  await p.keyboard.sendCharacter(ch);
  await new Promise((r) => setTimeout(r, 110));
}
await b.disconnect();
