import { Config } from "@remotion/cli/config";

// 同梱の chrome-headless-shell はこの Mac では起動直後に落ちる（Target closed）ので、
// インストール済みの Chrome で書き出す。
Config.setBrowserExecutable("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome");
Config.setChromeMode("chrome-for-testing");
