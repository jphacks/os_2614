// node tap.mjs <css selector | text:文字列>  : 要素の中心を adb でタップする（タップ表示を録画に残すため）
import puppeteer from "/Users/ringo/Documents/docs/projects/os_2614/web/node_modules/.pnpm/puppeteer-core@25.14.0_yauzl@2.10.0/node_modules/puppeteer-core/lib/puppeteer/puppeteer-core.js";
import { execFileSync } from "node:child_process";
const target = process.argv[2];
const b = await puppeteer.connect({ browserURL: "http://localhost:9222", defaultViewport: null });
const [p] = await b.pages();
const r = await p.evaluate((t) => {
  let el;
  if (t.startsWith("text:")) {
    const want = t.slice(5);
    const all = [...document.querySelectorAll("button,a,[role=button],[aria-label],span,div,p,h1,h2,h3")];
    el = all.filter((e) => (e.getAttribute("aria-label") === want || e.textContent.trim() === want) && e.getBoundingClientRect().width > 0).pop();
  } else el = document.querySelector(t);
  if (!el) return null;
  el.scrollIntoView({ block: "nearest" });
  const b = el.getBoundingClientRect();
  return { x: (b.left + b.width / 2) * devicePixelRatio, y: (b.top + b.height / 2) * devicePixelRatio, top: window.screenY };
}, target);
await b.disconnect();
if (!r) { console.error("not found", target); process.exit(1); }
// WebView は status bar の下から始まる
const off = Number(process.env.OFFSET_Y ?? 0);
execFileSync(process.env.HOME + "/Library/Android/sdk/platform-tools/adb", ["shell", "input", "tap", String(Math.round(r.x)), String(Math.round(r.y + off))]);
console.log("tap", Math.round(r.x), Math.round(r.y + off));
