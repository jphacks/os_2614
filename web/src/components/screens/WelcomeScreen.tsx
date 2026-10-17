"use client";

import { useState } from "react";
import { useAuth } from "@/auth";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import { Pressable } from "@/components/ui/Pressable";
import styles from "./WelcomeScreen.module.css";

export type WelcomeScreenProps = {
  onBluesky?: () => void;
  onEmail?: () => void;
};

export function WelcomeScreen({ onBluesky, onEmail }: WelcomeScreenProps) {
  const { state, login } = useAuth();
  const [loggingIn, setLoggingIn] = useState(false);
  const expired = state.status === "signedOut" && state.reason === "expired";

  const handleBluesky = async () => {
    if (loggingIn) {
      return;
    }
    setLoggingIn(true);
    try {
      onBluesky?.();
      await login();
    } catch {
      setLoggingIn(false);
    }
  };

  return (
    <div className={styles.screen}>
      {expired && (
        <div className={styles.notice}>
          <Notice icon="schedule">
            ログインの有効期限が切れました。もう一度ログインしてください
          </Notice>
        </div>
      )}

      <div className={styles.clouds} aria-hidden>
        <span className={`${styles.cloud} ${styles.cloudTopLeft}`} />
        <span className={`${styles.cloud} ${styles.cloudTopRight}`} />
        <span className={`${styles.cloud} ${styles.cloudBottomLeft}`} />
        <span className={`${styles.cloud} ${styles.cloudBottomRight}`} />
      </div>

      <div className={styles.center}>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          className={styles.character}
          src="/brand/charactor.svg"
          alt=""
          width={248}
          height={248}
        />
        <span className={styles.wordmark}>CHAIN</span>
      </div>

      <div className={styles.actions}>
        <Pressable
          className={`${styles.button} ${styles.bluesky}`}
          onClick={handleBluesky}
          disabled={loggingIn}
        >
          <span className={styles.buttonIcon} aria-hidden>
            <svg
              className={styles.blueskyLogo}
              viewBox="0 0 600 530"
              fill="currentColor"
            >
              <path d="m135.72 44.03c66.496 49.921 138.02 151.14 164.28 205.46 26.262-54.316 97.782-155.54 164.28-205.46 47.98-36.021 125.72-63.892 125.72 24.795 0 17.712-10.155 148.79-16.111 170.07-20.703 73.984-96.144 92.854-163.25 81.433 117.3 19.964 147.14 86.092 82.697 152.22-122.39 125.59-175.91-31.511-189.63-71.766-2.514-7.3797-3.6904-10.832-3.7077-7.8964-0.0174-2.9357-1.1937 0.51669-3.7077 7.8964-13.714 40.255-67.233 197.36-189.63 71.766-64.444-66.128-34.605-132.26 82.697-152.22-67.108 11.421-142.55-7.4491-163.25-81.433-5.9562-21.282-16.111-152.36-16.111-170.07 0-88.687 77.742-60.816 125.72-24.795z" />
            </svg>
          </span>
          <span className={styles.buttonLabel}>Blueskyで続ける</span>
        </Pressable>
        <Pressable
          className={`${styles.button} ${styles.email}`}
          onClick={() => onEmail?.()}
        >
          <span className={styles.buttonIcon} aria-hidden>
            <Icon name="mail" size={20} />
          </span>
          <span className={styles.buttonLabel}>メールアドレス</span>
        </Pressable>
      </div>
    </div>
  );
}
