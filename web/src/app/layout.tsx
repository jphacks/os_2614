import type { Metadata, Viewport } from "next";
import { Noto_Sans_JP } from "next/font/google";
import { AuthProvider } from "@/auth";
import { AuthGate } from "@/components/feature/auth";
import { StartupSplash } from "@/components/layout/StartupSplash";
import { NotificationToast } from "@/components/feature/notification";
import { PushBootstrap } from "@/device/push/PushBootstrap";
import { DeviceRegistrar } from "@/device/push/DeviceRegistrar";
import { ProximityDriver } from "@/components/layout/ProximityDriver";
import { PhotoShareDriver } from "@/components/layout/PhotoShareDriver";
import { RealtimeProvider } from "@/realtime/provider";
import "material-symbols/rounded.css";
import "./globals.css";

const notoSansJp = Noto_Sans_JP({
  variable: "--font-noto-sans-jp",
  weight: ["400", "500", "700", "900"],
  preload: false,
});

export const metadata: Metadata = {
  title: "CHAIN",
  description: "近くにいる人と、気軽につながる中くらいのSNS",
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
  themeColor: "#f5f5f5",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="ja" className={notoSansJp.variable} suppressHydrationWarning>
      <body>
        <StartupSplash />
        <AuthProvider>
          <AuthGate>
            <RealtimeProvider>
              <PushBootstrap />
              <DeviceRegistrar />
              <ProximityDriver />
              <PhotoShareDriver />
              <NotificationToast />
              {children}
            </RealtimeProvider>
          </AuthGate>
        </AuthProvider>
      </body>
    </html>
  );
}
