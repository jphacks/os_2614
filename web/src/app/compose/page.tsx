"use client";

// /compose は static export なので SSR ではデータを取らない。サインイン後に
// GET /instances (13 §2) で送信先を、GET /me でアバター用の著者を取得し、
// 「投稿する」で POST /posts (10 §4.1) へ送る。API エラーは ComposeSubmitError に
// 畳んで画面に出す。添付のアップロード (POST /media, 10 §4.4) は todo 15 が所有する。

import { Suspense, useMemo, useState } from "react";
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
  buildCreatePostBody,
  hasPendingAttachments,
  validateComposeInput,
  type ComposeSubmitInput,
} from "@/components/feature/compose";

/** 同意フロー (todo 35) の導線。画面ができたらそこへ差し替える。 */
const CONSENT_PATH = "/settings/";

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

function ComposeForm() {
  const router = useRouter();
  const params = useSearchParams();
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<ComposeSubmitError | null>(null);

  const { state: targetsState, reload: reloadTargets } = useResource<{
    items: InstanceDTO[];
  }>((signal) => getJson<{ items: InstanceDTO[] }>("/instances", { signal }), []);

  const { state: meState } = useResource<MeDTO>(
    (signal) => getJson<MeDTO>("/me", { signal }),
    [],
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

  const submit = async (input: ComposeSubmitInput) => {
    setError(null);
    // todo 15: 未アップロードの添付は media.id にならないため送らない（黙って落とさない）。
    if (hasPendingAttachments(input.attachments)) {
      setError({ kind: "unavailable" });
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
      const created = result.items[0];
      router.push(destinationFor(created?.spaceId ?? input.spaceIds[0], targetList));
    } catch (e) {
      setError(classifyPostError(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <ComposeScreen
      me={meState.status === "ok" ? toUser(meState.data) : undefined}
      targetsState={targets}
      initialSpaceIds={initialSpaceIds}
      submitting={submitting}
      error={error}
      onSubmit={submit}
      onConsent={() => router.push(CONSENT_PATH)}
      targetsRetry={reloadTargets}
    />
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
