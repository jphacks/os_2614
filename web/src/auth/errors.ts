// backend の { error: { code, message, details } } (backend/internal/httpx) を表す。

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: Record<string, unknown>;
  /** 429 の Retry-After を ms に直した値。無い / 不正なら undefined。 */
  readonly retryAfterMs?: number;

  constructor(
    status: number,
    code: string,
    message: string,
    details?: Record<string, unknown>,
    retryAfterMs?: number,
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.details = details;
    this.retryAfterMs = retryAfterMs;
  }
}

/**
 * Retry-After ヘッダを ms に直す。backend は 429 で秒数 (httpx/ratelimit.go) を返すが、
 * HTTP-date 形式も受け付ける。無い / 不正なら undefined。
 */
function parseRetryAfter(res: Response): number | undefined {
  const raw = res.headers.get("Retry-After");
  if (raw === null) {
    return undefined;
  }
  const trimmed = raw.trim();
  if (/^\d+$/.test(trimmed)) {
    return Number(trimmed) * 1000;
  }
  const at = Date.parse(trimmed);
  if (Number.isFinite(at)) {
    return Math.max(0, at - Date.now());
  }
  return undefined;
}

/** ログインしていない、またはセッションが失効した。画面はログインへ戻す。 */
export class SignedOutError extends Error {
  constructor() {
    super("signed out");
    this.name = "SignedOutError";
  }
}

export async function toApiError(res: Response): Promise<ApiError> {
  const retryAfterMs = parseRetryAfter(res);
  try {
    const body: unknown = await res.json();
    const e = (body as { error?: { code?: unknown; message?: unknown; details?: unknown } }).error;
    if (e && typeof e.code === "string") {
      return new ApiError(
        res.status,
        e.code,
        typeof e.message === "string" ? e.message : e.code,
        typeof e.details === "object" && e.details !== null
          ? (e.details as Record<string, unknown>)
          : undefined,
        retryAfterMs,
      );
    }
  } catch {
    // JSON でない応答 (Tunnel やプロキシのエラーページ) は下で包む。
  }
  return new ApiError(
    res.status,
    res.status >= 500 ? "internal" : "invalid_request",
    res.statusText,
    undefined,
    retryAfterMs,
  );
}
