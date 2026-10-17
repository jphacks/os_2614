"use client";

// /compose は static export なので SSR ではデータを取らない。サインイン後に
// GET /instances (13 §2) で送信先を、GET /me でアバター用の著者を取得する。
// 画像は選択時に POST /media (10 §4.4) へアップロードして id を得て、
// 「投稿する」で POST /posts (10 §4.1) の media に {id,alt} として載せる。
// API エラーは ComposeSubmitError に畳んで画面に出す。

import { Suspense, useMemo, useRef, useState, type ChangeEvent } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ApiError } from "@/auth/errors";
import { getJson, postJson } from "@/data/api";
import type {
  CreatePostResult,
  Instance as InstanceDTO,
  Me as MeDTO,
} from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toPostTarget, toUser } from "@/data/mappers";
import type { PostTarget } from "@/domain/types";
import {
  ComposeScreen,
  type ComposeSubmitError,
} from "@/components/screens/ComposeScreen";
import {
  ACCEPTED_MEDIA_TYPES,
  MAX_MEDIA_COUNT,
  buildCreatePostBody,
  hasPendingAttachments,
  uploadMedia,
  validateComposeInput,
  validateMediaFile,
  type ComposeAttachment,
  type ComposeSubmitInput,
} from "@/components/feature/compose";

/** 同意フロー (todo 35) の導線。 */
const CONSENT_PATH = "/consent/?next=compose";

/** 作成された投稿の Space に応じた戻り先。 */
function destinationFor(spaceId: string, targets: PostTarget[]): string {
  const target = targets.find((item) => item.spaceId === spaceId);
  if (target?.type === "party") {
    return "/party/";
  }
  if (target?.type === "group") {
    return `/group/?id=${encodeURIComponent(spaceId)}`;
  }
  return "/friends/";
}

/** POST /posts の失敗を画面の表示種別へ畳む。 */
function classifyPostError(error: unknown): ComposeSubmitError {
  if (error instanceof ApiError) {
    if (error.status === 403 && error.code === "consent_required") {
      return { kind: "consent" };
    }
    if (error.status === 404 || error.status === 405 || error.status === 501) {
      return { kind: "unavailable" };
    }
    if (error.status >= 400 && error.status < 500) {
      const fields = error.details?.fields;
      if (fields && typeof fields === "object" && "alsoPublic" in fields) {
        return {
          kind: "invalid",
          message: "公開投稿は現在利用できません。オフにして再試行してください。",
        };
      }
      return { kind: "invalid", message: error.message };
    }
    return { kind: "error", message: error.message };
  }
  return {
    kind: "error",
    message: error instanceof Error ? error.message : "投稿に失敗しました",
  };
}

/** POST /media の失敗を画面の表示種別へ畳む。 */
function classifyMediaError(error: unknown): ComposeSubmitError {
  if (error instanceof ApiError) {
    if (error.status === 413) {
      return { kind: "invalid", message: "画像は 15MB までです" };
    }
    if (error.status === 400) {
      return { kind: "invalid", message: error.message };
    }
    return { kind: "error", message: error.message };
  }
  return {
    kind: "error",
    message:
      error instanceof Error ? error.message : "画像をアップロードできませんでした",
  };
}

function ComposeForm() {
  const router = useRouter();
  const params = useSearchParams();
  const [submitting, setSubmitting] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<ComposeSubmitError | null>(null);
  const [attachments, setAttachments] = useState<ComposeAttachment[]>([]);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const { state: targetsState, reload: reloadTargets } = useResource<{
    items: InstanceDTO[];
  }>((signal) => getJson<{ items: InstanceDTO[] }>("/instances", { signal }), [], {
    cacheKey: "/instances",
  });

  const { state: meState } = useResource<MeDTO>(
    (signal) => getJson<MeDTO>("/me", { signal }),
    [],
    { cacheKey: "/me" },
  );

  // archived Space (13 §2 の memory) へは投稿できないので送信先から外す。
  const activeTargets = useMemo(
    () =>
      targetsState.status === "ok"
        ? targetsState.data.items.filter((item) => item.archivedAt == null)
        : [],
    [targetsState],
  );

  const targets: ResourceState<PostTarget[]> =
    targetsState.status !== "ok"
      ? targetsState
      : activeTargets.length === 0
        ? { status: "empty" }
        : { status: "ok", data: activeTargets.map(toPostTarget) };

  const targetList = targets.status === "ok" ? targets.data : [];

  // ?target=group:g1（GroupScreen / PartyScreen からの遷移）を初期送信先にする。
  const initialSpaceIds = useMemo(() => {
    const raw = params.get("target");
    if (!raw) {
      return [];
    }
    const [, spaceId] = raw.split(":");
    return spaceId ? [spaceId] : [];
  }, [params]);

  const pickAttachment = () => {
    fileInputRef.current?.click();
  };

  const removeAttachment = (key: string) => {
    setAttachments((prev) => {
      const target = prev.find((item) => item.key === key);
      if (target?.previewUrl?.startsWith("blob:")) {
        URL.revokeObjectURL(target.previewUrl);
      }
      return prev.filter((item) => item.key !== key);
    });
  };

  const onFilesSelected = async (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? []);
    // 同じファイルを選び直せるように毎回クリアする。
    event.target.value = "";
    if (files.length === 0) {
      return;
    }
    setError(null);

    const room = Math.max(0, MAX_MEDIA_COUNT - attachments.length);
    if (files.length > room) {
      setError({ kind: "invalid", message: "画像は 4 枚までです" });
    }

    // 事前検証を通った分だけ、プレビュー (object URL) を先に出してから送る。
    const entries: { file: File; key: string; previewUrl: string }[] = [];
    for (const file of files.slice(0, room)) {
      const check = validateMediaFile(file);
      if (!check.ok) {
        setError({ kind: "invalid", message: check.message });
        continue;
      }
      entries.push({ file, key: crypto.randomUUID(), previewUrl: URL.createObjectURL(file) });
    }
    if (entries.length === 0) {
      return;
    }

    setAttachments((prev) => [
      ...prev,
      ...entries.map((entry) => ({ key: entry.key, previewUrl: entry.previewUrl })),
    ]);
    setUploading(true);
    try {
      await Promise.all(
        entries.map(async (entry) => {
          try {
            const uploaded = await uploadMedia(entry.file);
            setAttachments((prev) =>
              prev.map((item) =>
                item.key === entry.key ? { ...item, id: uploaded.id } : item,
              ),
            );
          } catch (e) {
            setAttachments((prev) => prev.filter((item) => item.key !== entry.key));
            URL.revokeObjectURL(entry.previewUrl);
            setError(classifyMediaError(e));
          }
        }),
      );
    } finally {
      setUploading(false);
    }
  };

  const submit = async (input: ComposeSubmitInput) => {
    setError(null);
    // アップロード中の添付は media.id にならないため送らない（黙って落とさない）。
    if (uploading || hasPendingAttachments(input.attachments)) {
      setError({ kind: "invalid", message: "画像のアップロードが完了していません" });
      return;
    }
    const validation = validateComposeInput(input);
    if (!validation.ok) {
      setError({ kind: "invalid", message: validation.message });
      return;
    }
    setSubmitting(true);
    try {
      const result = await postJson<CreatePostResult>(
        "/posts",
        buildCreatePostBody(input),
      );
      if (result.public?.error) {
        setError({
          kind: "public",
          message:
            "公開投稿は作成できませんでした。Space への投稿は完了しています。",
        });
        return;
      }
      const created = result.items[0];
      router.push(destinationFor(created?.spaceId ?? input.spaceIds[0], targetList));
    } catch (e) {
      setError(classifyPostError(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <input
        ref={fileInputRef}
        type="file"
        accept={ACCEPTED_MEDIA_TYPES.join(",")}
        multiple
        hidden
        data-testid="compose-media-input"
        onChange={onFilesSelected}
      />
      <ComposeScreen
        me={meState.status === "ok" ? toUser(meState.data) : undefined}
        targetsState={targets}
        initialSpaceIds={initialSpaceIds}
        attachments={attachments}
        submitting={submitting}
        uploading={uploading}
        error={error}
        consentRequired={
          meState.status === "ok" &&
          meState.data.settings.contentScanConsentVersion !== 1
        }
        onSubmit={submit}
        onPickAttachment={pickAttachment}
        onRemoveAttachment={removeAttachment}
        onConsent={() => router.push(CONSENT_PATH)}
        targetsRetry={reloadTargets}
      />
    </>
  );
}

export default function ComposePage() {
  // useSearchParams は Suspense 境界の内側でだけ使える (static export の prerender 用)。
  return (
    <Suspense fallback={null}>
      <ComposeForm />
    </Suspense>
  );
}
