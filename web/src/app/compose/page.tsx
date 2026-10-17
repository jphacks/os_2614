import { Suspense } from "react";
import { ComposeScreen } from "@/components/screens/ComposeScreen";
import { me, postTargets } from "@/data/mock";

export default function ComposePage() {
  return (
    <Suspense fallback={null}>
      <ComposeScreen me={me} targets={postTargets} />
    </Suspense>
  );
}
