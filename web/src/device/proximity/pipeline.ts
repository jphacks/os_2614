// 近接パイプラインの純粋な判定 (05 §1-§4)。React にも native にも触らない。
// 実機を起動する側 (useProximityPipeline) は、ここで決めた「起動 / 停止 / 何もしない」と
// 「不足している権限」だけを見る。

import type { DevicePermission, PermissionStatus, ProximityError } from "./definitions";

/** すれ違いモードの走行に必要な権限。通知は push 用なので開始条件に含めない。 */
export const PROXIMITY_REQUIRED_PERMISSIONS = [
  "bluetooth",
  "location",
] as const satisfies readonly DevicePermission[];

/** 権限 UI からまとめて要求する権限 (05 §1)。 */
export const PROXIMITY_ALL_PERMISSIONS = [
  "bluetooth",
  "location",
  "notifications",
] as const satisfies readonly DevicePermission[];

/** 走行に必要な権限がすべて許可済みか。未取得 (null) は false。 */
export function proximityPermissionsReady(permissions: PermissionStatus | null): boolean {
  if (permissions === null) {
    return false;
  }
  return permissions.bluetooth === "granted" && permissions.location === "granted";
}

/** まだ許可されていない必須権限。権限 UI の説明に使う。 */
export function missingPermissions(permissions: PermissionStatus | null): DevicePermission[] {
  if (permissions === null) {
    return [...PROXIMITY_REQUIRED_PERMISSIONS];
  }
  return PROXIMITY_REQUIRED_PERMISSIONS.filter((name) => permissions[name] !== "granted");
}

/** パイプラインに取るべき行動。 */
export type PipelineAction = "start" | "stop" | "none";

/**
 * signedIn / 権限 / 走行状態から次の行動を決める。
 * - signedOut: 走っていれば stop (未ログインで残さない)
 * - signedIn + 権限あり + 未走行: start
 * - それ以外: none
 */
export function pipelineAction(input: {
  signedIn: boolean;
  running: boolean;
  permissionsReady: boolean;
}): PipelineAction {
  if (!input.signedIn) {
    return input.running ? "stop" : "none";
  }
  if (input.permissionsReady && !input.running) {
    return "start";
  }
  return "none";
}

const PROXIMITY_ERROR_MESSAGES: Record<ProximityError, string> = {
  permission_revoked: "権限が取り消されました。設定から許可し直してください。",
  bluetooth_off: "Bluetooth がオフになっています。オンにしてください。",
  bluetooth_unsupported: "この端末は Bluetooth に対応していません。",
  advertise_failed: "すれ違いのビーコンを発信できませんでした。",
  scan_failed: "すれ違いのスキャンに失敗しました。",
  tokens_exhausted: "すれ違い用のトークンが切れました。通信できる場所で再取得します。",
};

/** native のエラーコードを表示用の日本語にする。null は null。 */
export function describeProximityError(error: ProximityError | null): string | null {
  return error === null ? null : PROXIMITY_ERROR_MESSAGES[error];
}

/** 前面復帰 (hidden → visible) の瞬間か。visibilitychange の連続値から判定する。 */
export function isForegroundResume(previousHidden: boolean, hidden: boolean): boolean {
  return previousHidden && !hidden;
}

/** sighting 同期のポーリング間隔。5 分間隔と「未送信 50 件超」をこの粒度で検出する。 */
export const PROXIMITY_SYNC_POLL_MS = 30_000;
