"use client";

// signedIn のときだけ近接パイプラインを 1 本起動し、権限が足りなければ要求 UI を出す。
// AppShell から全画面に 1 つマウントされる (画面ごとに接続しない)。
// signedOut では ProximityDriverActive ごとアンマウントされ、hook の cleanup が
// native の stop とリスナー解除を行う。

import { useAuth } from "@/auth";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import { useProximityPipeline } from "@/device/proximity/useProximityPipeline";
import styles from "./ProximityDriver.module.css";

export function ProximityDriver() {
  const { state } = useAuth();
  if (state.status !== "signedIn") {
    return null;
  }
  return <ProximityDriverActive />;
}

function ProximityDriverActive() {
  const { status, ready, missing, error, requestPermissions } = useProximityPipeline();
  const driverState = !ready ? "blocked" : status?.running === true ? "running" : "idle";

  return (
    <div
      className={styles.driver}
      data-testid="proximity-driver"
      data-state={driverState}
      data-missing={missing.join(",")}
    >
      {!ready && missing.length > 0 ? (
        <div className={styles.banner} role="status">
          <Icon name="near_me" size={18} />
          <p className={styles.message}>
            すれ違い機能を使うには位置情報と Bluetooth の許可が必要です。
          </p>
          <Button
            variant="secondary"
            size="sm"
            icon="lock_open"
            onClick={requestPermissions}
          >
            許可する
          </Button>
        </div>
      ) : null}
      {error ? (
        <Notice icon="error" tone="warning">
          {error}
        </Notice>
      ) : null}
    </div>
  );
}
