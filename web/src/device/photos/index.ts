import { registerPlugin } from "@capacitor/core";
import type { ChainPhotosPlugin } from "./definitions";

type WebImplementation = import("./web").ChainPhotosWeb;
let webImplementation: Promise<WebImplementation> | undefined;

function loadWebImplementation(): Promise<WebImplementation> {
  webImplementation ??= import("./web").then((module) => new module.ChainPhotosWeb());
  return webImplementation;
}

export const ChainPhotos = registerPlugin<ChainPhotosPlugin>("ChainPhotos", {
  web: loadWebImplementation,
});

export * from "./definitions";
