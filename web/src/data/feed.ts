"use client";

import { deleteJson, postJson } from "./api";
import type { NearbyPostResult } from "./dto";

/** POST /feed/posts の応答 (service.PublicPost、17 §2.1)。 */
export type FeedPostRef = {
  uri: string;
  cid: string;
  bsky?: { uri: string };
};

export type CreateFeedPostInput = {
  text: string;
  langs?: string[];
  alsoBluesky?: boolean;
};

/** POST /api/v1/feed/posts (17 §2.1)。Space を持たない単独の公開投稿を作る。 */
export function createFeedPost(input: CreateFeedPostInput): Promise<FeedPostRef> {
  return postJson<FeedPostRef>("/feed/posts", input);
}

export type CreateNearbyPostInput = {
  text: string;
};

/** POST /api/v1/nearby/posts (17 §2.6)。今近くにいる人にだけ届く限定投稿を作る。 */
export function createNearbyPost(input: CreateNearbyPostInput): Promise<NearbyPostResult> {
  return postJson<NearbyPostResult>("/nearby/posts", input);
}

/** DELETE /api/v1/feed/posts/{rkey} (17 §2.2)。 */
export async function deleteFeedPost(rkey: string): Promise<void> {
  await deleteJson<void>(`/feed/posts/${encodeURIComponent(rkey)}`);
}
