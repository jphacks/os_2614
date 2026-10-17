import { Suspense } from "react";
import {
  AuthCallbackFallback,
  AuthCallbackScreen,
} from "@/components/screens/AuthCallbackScreen";

// useSearchParams は static export では Suspense 境界が必要。
export default function AuthCallbackPage() {
  return (
    <Suspense fallback={<AuthCallbackFallback />}>
      <AuthCallbackScreen />
    </Suspense>
  );
}
