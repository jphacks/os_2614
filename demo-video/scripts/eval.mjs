import puppeteer from "/Users/ringo/Documents/docs/projects/os_2614/web/node_modules/.pnpm/puppeteer-core@25.14.0_yauzl@2.10.0/node_modules/puppeteer-core/lib/puppeteer/puppeteer-core.js";
const b = await puppeteer.connect({ browserURL: "http://localhost:9222", defaultViewport: null });
const [p] = await b.pages();
console.log(JSON.stringify(await p.evaluate(process.argv[2])));
await b.disconnect();
