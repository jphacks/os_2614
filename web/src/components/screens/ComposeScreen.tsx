"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import type { PostTarget, User } from "@/domain/types";
import { CharCounterRing, TargetSelector } from "@/components/feature/compose";
import { Avatar } from "@/components/ui/Avatar";
import { Chip } from "@/components/ui/Chip";
import { Icon } from "@/components/ui/Icon";
import { IconButton } from "@/components/ui/IconButton";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { Notice } from "@/components/ui/Notice";
import styles from "./ComposeScreen.module.css";

const MAX_LENGTH = 140;

export type ComposeDraft = {
  targetId: string;
  body: string;
  attachments: File[];
};

export type ComposeScreenProps = {
  me: User;
  targets: PostTarget[];
  initialTargetId?: string;
  onSubmit?: (draft: ComposeDraft) => void;
  onCancel?: () => void;
};

function resolveTarget(
  targets: PostTarget[],
  param: string | null,
  fallbackId?: string,
): PostTarget {
  if (param) {
    const [kind, id] = param.split(":");
    const match = targets.find(
      (target) => target.kind === kind && target.id === id,
    );
    if (match) {
      return match;
    }
  }
  const byId = targets.find((target) => target.id === fallbackId);
  return byId ?? targets[0];
}

export function ComposeScreen({
  me,
  targets,
  initialTargetId,
  onSubmit,
  onCancel,
}: ComposeScreenProps) {
  const router = useRouter();
  const params = useSearchParams();
  const initialTarget = resolveTarget(targets, params.get("target"), initialTargetId);

  const [targetId, setTargetId] = useState<string>(initialTarget.id);
  const [body, setBody] = useState("");
  const [attachments, setAttachments] = useState<File[]>([]);
  const [menuOpen, setMenuOpen] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const target =
    targets.find((item) => item.id === targetId) ?? initialTarget;

  useEffect(() => {
    const node = textareaRef.current;
    if (node) {
      node.style.height = "auto";
      node.style.height = `${node.scrollHeight}px`;
    }
  }, [body]);

  const addAttachment = () => {
    const file = new File([new Uint8Array()], `photo-${attachments.length + 1}.jpg`, {
      type: "image/jpeg",
    });
    setAttachments((prev) => [...prev, file]);
  };

  const removeAttachment = (index: number) => {
    setAttachments((prev) => prev.filter((_, i) => i !== index));
  };

  const submit = () => {
    if (body.trim().length === 0) {
      return;
    }
    onSubmit?.({ targetId: target.id, body, attachments });
    router.push("/friends/");
  };

  const cancel = () => {
    onCancel?.();
    router.back();
  };

  const remaining = MAX_LENGTH - body.length;
  const hint =
    target.kind === "group"
      ? `グループのエリアにいる ${target.memberCount}人 のタイムラインに表示されます`
      : "パーティのメンバーに表示されます";

  return (
    <div className={styles.screen}>
      <header className={styles.header}>
        <button type="button" className={styles.cancel} onClick={cancel}>
          キャンセル
        </button>
        <button
          type="button"
          className={styles.submit}
          onClick={submit}
          disabled={body.trim().length === 0}
        >
          投稿する
        </button>
      </header>

      <div className={styles.body}>
        <div className={styles.targetRow}>
          <Avatar user={me} size={40} />
          <div className={styles.targetWrap}>
            <TargetSelector
              target={target}
              onClick={() => setMenuOpen((open) => !open)}
            />
            {menuOpen && (
              <div className={styles.menu} role="menu">
                {targets.map((item) => (
                  <button
                    key={`${item.kind}-${item.id}`}
                    type="button"
                    role="menuitemradio"
                    aria-checked={item.id === target.id}
                    className={styles.menuItem}
                    onClick={() => {
                      setTargetId(item.id);
                      setMenuOpen(false);
                    }}
                  >
                    <Icon
                      name={item.kind === "group" ? "hub" : "celebration"}
                      size={18}
                    />
                    {item.name}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        <textarea
          ref={textareaRef}
          className={styles.textarea}
          value={body}
          maxLength={MAX_LENGTH}
          placeholder="いまどうしてる？"
          aria-label="投稿本文"
          rows={1}
          onChange={(event) => setBody(event.target.value)}
        />

        {attachments.map((file, index) => (
          <div key={`${file.name}-${index}`} className={styles.attachment}>
            <ImagePlaceholder ratio="1/1" />
            <button
              type="button"
              className={styles.remove}
              aria-label="画像を削除"
              onClick={() => removeAttachment(index)}
            >
              <Icon name="close" size={16} />
            </button>
          </div>
        ))}

        <div className={styles.targets}>
          <span className={styles.caption}>送信先</span>
          <div className={styles.chipRow}>
            {targets.map((item) => (
              <Chip
                key={`${item.kind}-${item.id}`}
                label={item.name}
                icon={item.kind === "group" ? "hub" : "celebration"}
                selected={item.id === target.id}
                surface="surface"
                onClick={() => setTargetId(item.id)}
              />
            ))}
          </div>
          <p className={styles.hint}>{hint}</p>
        </div>
      </div>

      <footer className={styles.toolbar}>
        <div className={styles.tools}>
          <IconButton icon="image" label="画像を追加" onClick={addAttachment} />
          <IconButton
            icon="photo_camera"
            label="カメラで撮影"
            onClick={addAttachment}
          />
          <IconButton icon="location_on" label="位置を追加" />
          <IconButton icon="checklist" label="アンケートを追加" />
        </div>
        <div className={styles.counter}>
          <span className={styles.remaining}>あと {remaining}</span>
          <CharCounterRing value={body.length} max={MAX_LENGTH} />
        </div>
      </footer>

      <div className={styles.notice}>
        <Notice tone="info" icon="verified_user">
          投稿前に、誹謗中傷にならないかAIがチェックします
        </Notice>
      </div>
    </div>
  );
}
