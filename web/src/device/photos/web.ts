import { WebPlugin } from "@capacitor/core";
import type { ChainPhotosPlugin } from "./definitions";

/** ブラウザ用の写真モック。Playwright から保存結果を読める。 */
export class ChainPhotosWeb extends WebPlugin implements ChainPhotosPlugin {
  private saved = new Set<string>();
  private saveCalls = 0;

  constructor() {
    super();
    if (typeof window !== "undefined") {
      const bridge = ((window as unknown as { __chainPhotosMock?: Record<string, unknown> }).__chainPhotosMock ?? {}) as Record<string, unknown>;
      const calls = Array.isArray(bridge.calls) ? bridge.calls : [];
      bridge.calls = calls;
      bridge.state = () => ({ saved: [...this.saved], saveCalls: this.saveCalls, calls });
      bridge.ready = true;
      (window as unknown as { __chainPhotosMock?: unknown }).__chainPhotosMock = bridge;
    }
  }

  async savePhoto(_opts: {
    url: string;
    album: string;
    takenAt: string;
    fileName: string;
  }): Promise<{ mediaStoreId: string }> {
    void _opts;
    this.saveCalls += 1;
    this.recordCall("savePhoto");
    const mediaStoreId = `web-saved-${Date.now()}`;
    this.saved.add(mediaStoreId);
    return { mediaStoreId };
  }

  private recordCall(name: string): void {
    if (typeof window === "undefined") return;
    const bridge = (window as unknown as { __chainPhotosMock?: { calls?: string[] } }).__chainPhotosMock;
    bridge?.calls?.push(name);
    try {
      if (bridge?.calls) sessionStorage.setItem("chain.photo.test.calls", JSON.stringify(bridge.calls));
    } catch {
      // テスト用の記録が使えない環境でもモック本体は動かす。
    }
  }
}
