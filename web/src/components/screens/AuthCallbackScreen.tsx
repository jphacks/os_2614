"use client";

import { useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ApiError, useAuth, WELCOME_PATH } from "@/auth";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import styles from "./AuthCallbackScreen.module.css";

/** useSearchParams の読み込みが終わるまでの表示。 */
export function AuthCallbackFallback() {
  return (
    <div className={styles.screen}>
      <div className={styles.content}>
        <p className={styles.message}>ログインしています…</p>
      </div>
    </div>
  );
}

/** one-time code を交換する。同じ code の交換は auth 側で 1 本にまとめられている。 */
export function AuthCallbackScreen() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { completeLogin } = useAuth();

  const code = searchParams.get("code");
  const errorParam = searchParams.get("error");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  useEffect(() => {
    if (!code) {
      return;
    }
    let cancelled = false;
    completeLogin(code).then(
      () => {
        if (!cancelled) {
          router.replace("/");
        }
      },
      (error: unknown) => {
        if (cancelled) {
          return;
        }
        if (
          error instanceof ApiError &&
          (error.code === "unauthenticated" || error.code === "invalid_request")
        ) {
          setErrorMessage("ログインの有効期限が切れました");
        } else {
          setErrorMessage("ログインできませんでした");
        }
      },
    );
    return () => {
      cancelled = true;
    };
  }, [code, completeLogin, router]);

  useEffect(() => {
    if (!code && !errorParam) {
      router.replace(WELCOME_PATH);
    }
  }, [code, errorParam, router]);

  const failed = Boolean(errorParam) || errorMessage !== null;
  const message = errorParam
    ? "ログインできませんでした"
    : (errorMessage ?? "ログインしています…");

  return (
    <div className={styles.screen}>
      <div className={styles.content}>
        <Icon
          name={failed ? "error" : "progress_activity"}
          size={40}
          className={styles.icon}
        />
        <p className={styles.message}>{message}</p>
        {failed && (
          <>
            <p className={styles.description}>もう一度ログインしてください。</p>
            {errorParam && <span className={styles.errorCode}>{errorParam}</span>}
            <Button
              variant="primary"
              className={styles.action}
              onClick={() => router.replace(WELCOME_PATH)}
            >
              もう一度ログイン
            </Button>
          </>
        )}
      </div>
    </div>
  );
}
