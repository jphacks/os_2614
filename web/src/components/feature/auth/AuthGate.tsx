"use client";

import { useEffect, type ReactNode } from "react";
import { usePathname, useRouter } from "next/navigation";
import { CALLBACK_PATH, useAuth, WELCOME_PATH } from "@/auth";

/** trailingSlash の有無を同じパスとして扱う。 */
function normalize(path: string): string {
  return path.length > 1 && path.endsWith("/") ? path.slice(0, -1) : path;
}

const PUBLIC_PATHS = new Set([normalize(WELCOME_PATH), normalize(CALLBACK_PATH)]);
const HOME_PATH = "/";
const WELCOME = normalize(WELCOME_PATH);

/**
 * 認証状態に応じて画面を出し分ける。公開画面 (welcome / callback) は常に描画し、
 * それ以外は signedIn のときだけ children を描画する。リダイレクトは描画中ではなく effect で行う。
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { state } = useAuth();

  const path = normalize(pathname);
  const isPublic = PUBLIC_PATHS.has(path);
  const isSignedIn = state.status === "signedIn";
  const isSignedOut = state.status === "signedOut";

  useEffect(() => {
    if (state.status === "loading") {
      return;
    }
    if (!isPublic && isSignedOut) {
      router.replace(WELCOME_PATH);
      return;
    }
    if (path === WELCOME && isSignedIn) {
      router.replace(HOME_PATH);
    }
  }, [state.status, isPublic, isSignedOut, isSignedIn, path, router]);

  if (isPublic) {
    return children;
  }
  return isSignedIn ? children : null;
}
