import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    // Capacitor が out/ をコピーする生成物。バンドル済み JS を lint しない
    "android/**",
    "next-env.d.ts",
    // Playwright の生成レポート (バンドル済み JS を lint しない)
    "playwright-report/**",
    "test-results/**",
  ]),
]);

export default eslintConfig;
