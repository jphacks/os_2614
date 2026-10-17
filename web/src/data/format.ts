// 表示用の整形。純関数。ドメイン型 (Timestamp は ISO 文字列) を入力にする。

export type TimeInput = string | number | Date;

const MINUTE = 60_000;
const HOUR = 3_600_000;
const DAY = 86_400_000;

function toMillis(value: TimeInput): number {
  if (value instanceof Date) {
    return value.getTime();
  }
  if (typeof value === "number") {
    return value;
  }
  return new Date(value).getTime();
}

function pad2(value: number): string {
  return value < 10 ? `0${value}` : `${value}`;
}

function startOfDay(date: Date): number {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

/** 例: "10分前" "1時間前" "きのう" */
export function relativeTime(ts: TimeInput, now: TimeInput): string {
  const target = toMillis(ts);
  const current = toMillis(now);
  if (!Number.isFinite(target) || !Number.isFinite(current)) {
    return "";
  }

  const diff = current - target;
  if (diff < MINUTE) {
    return "たった今";
  }
  if (diff < HOUR) {
    return `${Math.floor(diff / MINUTE)}分前`;
  }
  if (diff < DAY) {
    return `${Math.floor(diff / HOUR)}時間前`;
  }

  const dayDiff = Math.round(
    (startOfDay(new Date(current)) - startOfDay(new Date(target))) / DAY,
  );
  if (dayDiff <= 1) {
    return "きのう";
  }
  if (dayDiff < 7) {
    return `${dayDiff}日前`;
  }

  const date = new Date(target);
  return `${date.getMonth() + 1}/${date.getDate()}`;
}

/** 例: "09:12" */
export function clockTime(ts: TimeInput): string {
  const millis = toMillis(ts);
  if (!Number.isFinite(millis)) {
    return "";
  }
  const date = new Date(millis);
  return `${pad2(date.getHours())}:${pad2(date.getMinutes())}`;
}

/** 例: "01:23:45" */
export function elapsed(startTs: TimeInput, now: TimeInput): string {
  const start = toMillis(startTs);
  const current = toMillis(now);
  if (!Number.isFinite(start) || !Number.isFinite(current)) {
    return "00:00:00";
  }
  const total = Math.max(0, Math.floor((current - start) / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  return `${pad2(hours)}:${pad2(minutes)}:${pad2(seconds)}`;
}

/** 例: "10/3–10/4" */
export function dateRange(start: TimeInput, end: TimeInput): string {
  const startMillis = toMillis(start);
  const endMillis = toMillis(end);
  if (!Number.isFinite(startMillis) || !Number.isFinite(endMillis)) {
    return "";
  }
  const startDate = new Date(startMillis);
  const endDate = new Date(endMillis);
  return `${startDate.getMonth() + 1}/${startDate.getDate()}–${
    endDate.getMonth() + 1
  }/${endDate.getDate()}`;
}

/** 例: "2日間" "3時間" */
export function durationLabel(start: TimeInput, end: TimeInput): string {
  const startMillis = toMillis(start);
  const endMillis = toMillis(end);
  if (!Number.isFinite(startMillis) || !Number.isFinite(endMillis)) {
    return "";
  }
  const diff = Math.max(0, endMillis - startMillis);
  const days = Math.floor(diff / DAY);
  if (days >= 1) {
    return `${days}日間`;
  }
  const hours = Math.floor(diff / HOUR);
  if (hours >= 1) {
    return `${hours}時間`;
  }
  return `${Math.max(1, Math.floor(diff / MINUTE))}分`;
}
