import { describe, expect, it } from "bun:test";
import { usePreferencesStore, ZOOM_LEVELS } from "./use-preferences-store";

describe("usePreferencesStore", () => {
  it("starts with sound and the new-order chime on at normal size", () => {
    const s = usePreferencesStore.getState();
    expect(s.soundEnabled).toBe(true);
    expect(s.newOrderChime).toBe(true);
    expect(s.zoom).toBe(100);
  });

  it("offers the five zoom steps the settings screen shows", () => {
    expect(ZOOM_LEVELS).toEqual([90, 100, 125, 150, 200]);
  });

  it("changes each preference independently", () => {
    const s = usePreferencesStore.getState();
    s.setZoom(125);
    s.setNewOrderChime(false);
    expect(usePreferencesStore.getState()).toMatchObject({ zoom: 125, newOrderChime: false, soundEnabled: true });
  });
});
