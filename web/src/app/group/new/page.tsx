"use client";

// /group/new は static export なので SSR ではデータを取らない。サインイン後に
// GET /friends から招待候補を、useLocation から現在地を取って、
// POST /groups (09 §2) に spec body を送る。

import { ApiError } from "@/auth";
import { postForm, postJson, toResult } from "@/data/api";
import type {
  Friend as FriendDTO,
  Group as GroupDTO,
  GroupCreateRequest,
  MediaUpload,
} from "@/data/dto";
import { usePagedResource } from "@/data/hooks";
import { toUser } from "@/data/mappers";
import { useLocation } from "@/device/proximity/hooks";
import type { User } from "@/domain/types";
import {
  CreateGroupScreen,
  type CreateGroupDraft,
  type CreateGroupResult,
  type SimilarGroup,
} from "@/components/screens";

const LOCATION_INTERVAL_MS = 3_000;

/** 409 conflict の details.similar (09 §2) を取り出す。形が違えば空配列。 */
function readSimilar(error: ApiError): SimilarGroup[] {
  const raw = error.details?.similar;
  if (!Array.isArray(raw)) {
    return [];
  }
  const similar: SimilarGroup[] = [];
  for (const item of raw) {
    if (item && typeof item === "object") {
      const { id, name } = item as { id?: unknown; name?: unknown };
      if (typeof id === "string" && typeof name === "string") {
        similar.push({ id, name });
      }
    }
  }
  return similar;
}

/** 400 の details.fields (項目 → 理由) を添えて、どの入力が弾かれたか分かるようにする。 */
function describeError(error: ApiError): string {
  const fields = error.details?.fields;
  if (fields && typeof fields === "object") {
    const reasons = Object.entries(fields).map(([key, value]) => `${key}: ${String(value)}`);
    if (reasons.length > 0) {
      return `${error.message} (${reasons.join(", ")})`;
    }
  }
  return error.message;
}

export default function CreateGroupPage() {
  const { fix, error: locationError } = useLocation({
    intervalMs: LOCATION_INTERVAL_MS,
    enabled: true,
  });
  const { items } = usePagedResource<FriendDTO>("/friends", []);

  const candidates: User[] = items.map(toUser);
  const location = fix ? { lat: fix.lat, lon: fix.lon } : null;

  const submit = async (draft: CreateGroupDraft): Promise<CreateGroupResult> => {
    if (!fix) {
      return { status: "location_stale" };
    }
    const body: GroupCreateRequest = {
      name: draft.name,
      description: draft.description,
      visibility: draft.visibility,
      persistent: draft.persistent,
      geofence: { lat: fix.lat, lon: fix.lon, radiusM: draft.radiusM },
      inviteQuorum: draft.inviteQuorum,
      force: draft.force,
      iconMediaId: draft.iconMediaId,
    };
    const result = await toResult(() => postJson<GroupDTO>("/groups", body));
    switch (result.status) {
      case "ok":
        return { status: "ok", groupId: result.data.id };
      case "unavailable":
        return { status: "unavailable" };
      case "error":
        if (result.error.status === 409 && result.error.code === "conflict") {
          return { status: "conflict", similar: readSimilar(result.error) };
        }
        if (result.error.status === 403 && result.error.code === "location_stale") {
          return { status: "location_stale" };
        }
        return { status: "error", message: describeError(result.error) };
    }
  };

  const uploadIcon = async (file: File): Promise<string | null> => {
    const form = new FormData();
    form.append("file", file);
    const result = await toResult(() => postForm<MediaUpload>("/media", form));
    return result.status === "ok" ? result.data.id : null;
  };

  return (
    <CreateGroupScreen
      candidates={candidates}
      location={location}
      locationError={locationError}
      onSubmit={submit}
      onUploadIcon={uploadIcon}
    />
  );
}
