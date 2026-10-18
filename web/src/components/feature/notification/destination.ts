/**
 * 通知の種類と data から開く画面を決める（12 §4）。OS 通知・トースト・通知一覧のタップで共通に使う。
 * FCM の data は type / targetId / spaceId だけ、WebSocket と GET /notifications は payload そのまま
 * （actor・inviteId など）が来るので、どちらでも引けるように両方を見る。
 */
export function destinationOf(type: string, data: Record<string, unknown> = {}): string {
  const spaceId = str(data.spaceId) || str(data.targetId);
  const group = (path: string) =>
    spaceId ? `${path}?id=${encodeURIComponent(spaceId)}` : "/notifications/";

  switch (type) {
    case "friend.request":
      return "/friends/requests/";
    case "friend.accepted": {
      const did = actorDid(data.actor);
      return did ? `/users/?did=${encodeURIComponent(did)}&relation=friend` : "/friends/";
    }
    case "party.confirmed":
      return "/party/";
    case "group.invited":
      // 自分宛ての招待は id 無しの招待画面に出る（受け取る側はメンバー向けの一覧を読めない）。
      return "/group/invites/";
    case "group.invite_approval":
      return group("/group/invites/");
    case "post.new":
    case "group.nearby":
    case "group.suggested":
    case "group.auto_joined":
    case "group.vote_passed":
    case "moderation.hidden":
      return group("/group/");
    case "encounter.daily":
    case "encounter.weekly":
    case "encounter.monthly":
      return "/encounters/";
    default:
      return "/notifications/";
  }
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function actorDid(actor: unknown): string {
  return actor && typeof actor === "object" ? str((actor as { did?: unknown }).did) : "";
}
