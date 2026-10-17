import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Capacitor の WebView に out/ を読ませるため static export にする (SSR 不可)
  output: "export",
  // Capacitor のローカルサーバは /me を /me.html に解決しないので /me/index.html で出力する
  trailingSlash: true,
  // static export では既定ローダの Image Optimization が使えない
  images: { unoptimized: true },
  reactCompiler: true,
  // 端末の WebView でライブリロードするとき、開発用インジケータがタブバーに重なるため隠す
  devIndicators: false,
};

export default nextConfig;
