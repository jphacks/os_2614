"use client";

import { useEffect, useSyncExternalStore, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { Capacitor } from "@capacitor/core";
import { CALLBACK_PATH } from "./config";
import { listenForAuthCallback, startLogin } from "./login";
import { session, type AuthState } from "./session";

/** native の deep link を CALLBACK_PATH の画面へ渡す。セッション自体はモジュールの singleton が持つ。 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const router = useRouter();

  useEffect(() => {
    if (!Capacitor.isNativePlatform()) {
      return;
    }
    return listenForAuthCallback((params) => {
      router.replace(`${CALLBACK_PATH}?${params.toString()}`);
    });
  }, [router]);

  return children;
}

export interface UseAuth {
  state: AuthState;
  login: (handle?: string) => Promise<void>;
  logout: () => Promise<void>;
  /** one-time code を交換する。CALLBACK_PATH の画面からだけ呼ぶ */
  completeLogin: (code: string) => Promise<void>;
}

const actions = {
  login: startLogin,
  logout: () => session.logout(),
  completeLogin: (code: string) => session.completeLogin(code),
};

export function useAuth(): UseAuth {
  const state = useSyncExternalStore(session.subscribe, session.getState, session.getServerState);
  return { state, ...actions };
}
