import { create } from "zustand";
import { persist } from "zustand/middleware";

export const ZOOM_LEVELS = [90, 100, 125, 150, 200] as const;
export type ZoomLevel = (typeof ZOOM_LEVELS)[number];

/**
 * Per-device preferences shown on the "Cài đặt" screen. They belong to the
 * terminal, not to the signed-in staff member, so they live in localStorage.
 * The theme is kept by next-themes under its own key.
 */
export interface PreferencesState {
  soundEnabled: boolean;
  newOrderChime: boolean;
  zoom: ZoomLevel;
  setSoundEnabled: (enabled: boolean) => void;
  setNewOrderChime: (enabled: boolean) => void;
  setZoom: (zoom: ZoomLevel) => void;
}

export const usePreferencesStore = create<PreferencesState>()(
  persist(
    (set) => ({
      soundEnabled: true,
      newOrderChime: true,
      zoom: 100,
      setSoundEnabled: (soundEnabled) => set({ soundEnabled }),
      setNewOrderChime: (newOrderChime) => set({ newOrderChime }),
      setZoom: (zoom) => set({ zoom }),
    }),
    { name: "pos_preferences" },
  ),
);
