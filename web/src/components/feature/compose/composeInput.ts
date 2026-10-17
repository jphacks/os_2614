// Compose の送信入力と `POST /posts` の body（10 §4.1）を 1 箇所にまとめる純関数。
// 画面（ComposeScreen）と e2e が同じ規則を見るように、検証と body 生成はここだけに置く。
//
// 規則は backend `internal/controller/instance.go` の `createPostRequest.Validate` に合わせる:
//   - spaceIds は 1〜5 件（alsoPublic なら 0 件も可。そのときは POST /feed/posts の単独公開投稿、17 §2.1）
//   - media は 4 件まで、かつ spaceIds が 1 件のときだけ
//   - text が空なら media が必要
//   - text は 2000 ルーンまで（Go の utf8.RuneCountInString と同じ数え方）
// 140 字は UI の目安でしかない（サーバ上限ではない）。

import type { PostFormat } from "@/data/dto";

/**
 * 画面が持つ添付 1 件。`id` は `POST /media`（10 §4.4、todo 15）が返した値だけが入る。
 * `id` が無い添付は「未アップロード」で、送信時に media へは載せない（黙って落とさない）。
 */
export type ComposeAttachment = {
  /** 画面で一意なキー（サーバ id ではない）。 */
  key: string;
  /** `POST /media` 済みの media id。未アップロードなら undefined。 */
  id?: string;
  alt?: string;
  /** プレビュー URL（署名付き URL か object URL）。 */
  previewUrl?: string;
};

/** 画面が `onSubmit` に渡す入力。 */
export type ComposeSubmitInput = {
  spaceIds: string[];
  text: string;
  format: PostFormat;
  attachments: ComposeAttachment[];
  alsoPublic: boolean;
};

/** 検証結果。NG のときだけ理由を持つ。 */
export type ComposeValidation = { ok: true } | { ok: false; message: string };

/** `POST /media`（10 §4.4）が受け付ける MIME。仕様は JPEG / PNG / WebP。 */
export const ACCEPTED_MEDIA_TYPES = ["image/jpeg", "image/png", "image/webp"] as const;

/** 添付画像 1 枚の上限（10 §4.4 の 15 MB）。 */
export const MAX_MEDIA_BYTES = 15 * 1024 * 1024;

/** 1 投稿に添付できる画像の枚数（10 §4.1: media は 4 件まで）。 */
export const MAX_MEDIA_COUNT = 4;

/** `validateMediaFile` が見る最小の形（`File` のサブセット。テストで作りやすい）。 */
export type MediaFileLike = { type: string; size: number };

/**
 * 画像ファイルの事前検証（10 §4.4）。JPEG / PNG / WebP かつ 15 MB まで。
 * サーバの 400 / 413 になる前にクライアントで拒否する（EXIF は見ない。落とすのはサーバ）。
 */
export function validateMediaFile(file: MediaFileLike): ComposeValidation {
  if (!(ACCEPTED_MEDIA_TYPES as readonly string[]).includes(file.type)) {
    return { ok: false, message: "JPEG・PNG・WebP の画像を選んでください" };
  }
  if (file.size > MAX_MEDIA_BYTES) {
    return { ok: false, message: "画像は 15MB までです" };
  }
  return { ok: true };
}

/** `POST /posts` の body（10 §4.1）。キーはこの 5 つだけ。 */
export type CreatePostBody = {
  spaceIds: string[];
  text: string;
  format: PostFormat;
  media: { id: string; alt: string }[];
  alsoPublic: boolean;
};

/** Go の `utf8.RuneCountInString` に合わせたルーン数（サロゲートペアを 1 と数える）。 */
export function runeCount(text: string): number {
  return Array.from(text).length;
}

/** 単独の公開投稿の本文上限（17 §2.1 の 1000 grapheme）。 */
export const MAX_PUBLIC_GRAPHEMES = 1000;

/** grapheme 数。Intl.Segmenter が無い環境ではルーン数で近似する。 */
export function graphemeCount(text: string): number {
  if (typeof Intl !== "undefined" && "Segmenter" in Intl) {
    const segmenter = new Intl.Segmenter("ja", { granularity: "grapheme" });
    return Array.from(segmenter.segment(text)).length;
  }
  return runeCount(text);
}

/** Space を選ばず公開だけで投稿する（POST /feed/posts、17 §2.1）か。 */
export function isPublicOnly(input: ComposeSubmitInput): boolean {
  return input.alsoPublic && input.spaceIds.length === 0;
}

/** 送信可能かを検証する。サーバの 400 になる前にクライアントで拒否する。 */
export function validateComposeInput(input: ComposeSubmitInput): ComposeValidation {
  const { spaceIds, text, attachments } = input;
  if (isPublicOnly(input)) {
    if (attachments.length > 0) {
      return { ok: false, message: "公開だけの投稿には画像を付けられません" };
    }
    if (text.trim().length === 0) {
      return { ok: false, message: "本文が必要です" };
    }
    if (graphemeCount(text) > MAX_PUBLIC_GRAPHEMES) {
      return { ok: false, message: "公開投稿は 1000 文字までです" };
    }
    return { ok: true };
  }
  if (spaceIds.length < 1 || spaceIds.length > 5) {
    return { ok: false, message: "送信先は 1〜5 件で選んでください" };
  }
  if (attachments.length > 0 && spaceIds.length !== 1) {
    return {
      ok: false,
      message: "画像を付けられるのは送信先が 1 つのときだけです",
    };
  }
  if (attachments.length > MAX_MEDIA_COUNT) {
    return { ok: false, message: "画像は 4 枚までです" };
  }
  if (text.trim().length === 0 && attachments.length === 0) {
    return { ok: false, message: "本文か画像が必要です" };
  }
  if (runeCount(text) > 2000) {
    return { ok: false, message: "本文は 2000 文字までです" };
  }
  return { ok: true };
}

/**
 * 送信入力から body を作る。media は `id` を持つ添付（アップロード済み）だけを載せる。
 * 未アップロードの添付が混ざっていても黙って落とさないよう、呼び出し側が先に確認する
 * （page.tsx の submit を参照）。
 */
export function buildCreatePostBody(input: ComposeSubmitInput): CreatePostBody {
  return {
    spaceIds: [...input.spaceIds],
    text: input.text,
    format: input.format,
    media: input.attachments
      .filter((attachment) => attachment.id !== undefined && attachment.id !== "")
      .map((attachment) => ({ id: attachment.id as string, alt: attachment.alt ?? "" })),
    alsoPublic: input.alsoPublic,
  };
}

/** 送信前に未アップロードの添付が残っていないか。 */
export function hasPendingAttachments(attachments: ComposeAttachment[]): boolean {
  return attachments.some(
    (attachment) => attachment.id === undefined || attachment.id === "",
  );
}
