"use client";

import { useEffect, useState } from "react";

const STORAGE_KEY = "chain.party.photos.settings.v1";
const CHANGE_EVENT = "chain-party-photo-settings-changed";

export type PhotoSettings = {
  saveToDevice: boolean;
};

export const DEFAULT_PHOTO_SETTINGS: PhotoSettings = {
  saveToDevice: false,
};

export function readPhotoSettings(): PhotoSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) {
      return DEFAULT_PHOTO_SETTINGS;
    }
    const parsed = JSON.parse(raw) as Partial<PhotoSettings>;
    return {
      saveToDevice: parsed.saveToDevice === true,
    };
  } catch {
    return DEFAULT_PHOTO_SETTINGS;
  }
}

export function writePhotoSettings(next: PhotoSettings): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    window.dispatchEvent(new Event(CHANGE_EVENT));
  } catch {
    // private browsing などで保存できない場合も、画面上の状態は維持する。
  }
}

export function clearPhotoSettings(): void {
  try {
    localStorage.removeItem(STORAGE_KEY);
    window.dispatchEvent(new Event(CHANGE_EVENT));
  } catch {
    // logout は保存失敗でも続行する。
  }
}

export function usePhotoSettings(): [PhotoSettings, (next: PhotoSettings) => void] {
  const [settings, setSettings] = useState<PhotoSettings>(DEFAULT_PHOTO_SETTINGS);
  useEffect(() => {
    const load = () => setSettings(readPhotoSettings());
    load();
    window.addEventListener(CHANGE_EVENT, load);
    return () => window.removeEventListener(CHANGE_EVENT, load);
  }, []);
  return [settings, (next) => {
    setSettings(next);
    writePhotoSettings(next);
  }];
}
