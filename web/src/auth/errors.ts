// backend の { error: { code, message, details } } (backend/internal/httpx) を表す。

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: Record<string, unknown>;

  constructor(status: number, code: string, message: string, details?: Record<string, unknown>) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

/** ログインしていない、またはセッションが失効した。画面はログインへ戻す。 */
export class SignedOutError extends Error {
  constructor() {
    super("signed out");
    this.name = "SignedOutError";
  }
}

export async function toApiError(res: Response): Promise<ApiError> {
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
      );
    }
  } catch {
    // JSON でない応答 (Tunnel やプロキシのエラーページ) は下で包む。
  }
  return new ApiError(res.status, res.status >= 500 ? "internal" : "invalid_request", res.statusText);
}
