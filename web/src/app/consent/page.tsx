"use client";

import { Suspense, useCallback, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { getJson, putJson, toResult } from "@/data/api";
import type { Me } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { ConsentScreen, type ConsentState } from "@/components/screens";

function ConsentForm() {
  const router = useRouter();
  const params = useSearchParams();
  const { state, reload } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const consentState: ResourceState<ConsentState> =
    state.status === "ok"
      ? {
          status: "ok",
          data: { version: state.data.settings.contentScanConsentVersion, consentedAt: null },
        }
      : state;

  const mutate = useCallback(
    async (contentScan: number) => {
      setError(null);
      setSaving(true);
      const result = await toResult(() =>
        putJson<{ contentScan: number; consentedAt: string | null }>("/me/consents", {
          contentScan,
        }),
      );
      setSaving(false);
      if (result.status === "ok") {
        reload();
        if (params.get("next") === "compose") {
          router.push("/compose/");
        }
        return;
      }
      setError(
        result.status === "unavailable"
          ? "同意はまだ利用できません。"
          : result.error.message,
      );
    },
    [params, reload, router],
  );

  return (
    <ConsentScreen
      state={consentState}
      saving={saving}
      error={error}
      onAgree={() => void mutate(1)}
      onWithdraw={() => void mutate(0)}
      onRetry={reload}
    />
  );
}

export default function ConsentPage() {
  return (
    <Suspense fallback={null}>
      <ConsentForm />
    </Suspense>
  );
}
