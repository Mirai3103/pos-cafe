import { beforeEach, describe, expect, it } from "bun:test";
import { getSoundEnabled, setSoundEnabled, playTapChirp, playSuccessChirp, playErrorBuzz } from "./sound";

describe("sound utility", () => {
  beforeEach(() => {
    try {
      localStorage.clear();
    } catch {}
  });

  it("defaults sound enabled to true", () => {
    expect(getSoundEnabled()).toBe(true);
  });

  it("updates and retrieves sound enabled state", () => {
    setSoundEnabled(false);
    expect(getSoundEnabled()).toBe(false);
    setSoundEnabled(true);
    expect(getSoundEnabled()).toBe(true);
  });

  it("handles sound playback functions gracefully in non-browser environments without errors", () => {
    expect(() => playTapChirp()).not.toThrow();
    expect(() => playSuccessChirp()).not.toThrow();
    expect(() => playErrorBuzz()).not.toThrow();
  });
});
