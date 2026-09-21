import * as React from "react";

const SOUND_STORAGE_KEY = "pos_sound_enabled";

let sharedAudioCtx: AudioContext | null = null;
let memorySoundEnabled: boolean | null = null;

export function getSoundEnabled(): boolean {
  try {
    const val = localStorage.getItem(SOUND_STORAGE_KEY);
    return val === null ? (memorySoundEnabled ?? true) : val === "true";
  } catch {
    return memorySoundEnabled ?? true;
  }
}

export function setSoundEnabled(enabled: boolean): void {
  memorySoundEnabled = enabled;
  try {
    localStorage.setItem(SOUND_STORAGE_KEY, String(enabled));
  } catch {
    // Storage access may be restricted
  }
}

export function getAudioContext(): AudioContext | null {
  try {
    if (typeof window === "undefined") return null;
    const AudioCtx =
      window.AudioContext ||
      (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
    if (!AudioCtx) return null;
    if (!sharedAudioCtx) {
      sharedAudioCtx = new AudioCtx();
    }
    if (sharedAudioCtx.state === "suspended") {
      void sharedAudioCtx.resume();
    }
    return sharedAudioCtx;
  } catch {
    return null;
  }
}

/** Tactile key tap: 800Hz gentle sine chirp (30ms) ported from design-system/pos-cafe/pages/auth.html */
export function playTapChirp(): void {
  if (!getSoundEnabled()) return;
  try {
    const ctx = getAudioContext();
    if (!ctx) return;
    const now = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = "sine";
    osc.frequency.setValueAtTime(800, now);
    gain.gain.setValueAtTime(0.08, now);
    gain.gain.exponentialRampToValueAtTime(0.001, now + 0.03);
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.start(now);
    osc.stop(now + 0.03);
  } catch {
    // Ignore sound playback errors
  }
}

/** Success sign-in / action: ascending chime (523Hz -> 659Hz -> 784Hz) */
export function playSuccessChirp(): void {
  if (!getSoundEnabled()) return;
  try {
    const ctx = getAudioContext();
    if (!ctx) return;
    const now = ctx.currentTime;
    const notes = [523.25, 659.25, 783.99]; // C5, E5, G5
    notes.forEach((freq, idx) => {
      const noteTime = now + idx * 0.09;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = "sine";
      osc.frequency.setValueAtTime(freq, noteTime);
      gain.gain.setValueAtTime(0.12, noteTime);
      gain.gain.exponentialRampToValueAtTime(0.001, noteTime + 0.28);
      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.start(noteTime);
      osc.stop(noteTime + 0.28);
    });
  } catch {
    // Ignore sound playback errors
  }
}

/** Error buzz: 200Hz sawtooth wave buzz (150ms) */
export function playErrorBuzz(): void {
  if (!getSoundEnabled()) return;
  try {
    const ctx = getAudioContext();
    if (!ctx) return;
    const now = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = "sawtooth";
    osc.frequency.setValueAtTime(200, now);
    gain.gain.setValueAtTime(0.15, now);
    gain.gain.exponentialRampToValueAtTime(0.001, now + 0.15);
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.start(now);
    osc.stop(now + 0.15);
  } catch {
    // Ignore sound playback errors
  }
}

/** React hook for listening and toggling sound feedback state */
export function useSound() {
  const [enabled, setEnabledState] = React.useState<boolean>(getSoundEnabled);

  const toggle = React.useCallback(() => {
    setEnabledState((prev) => {
      const next = !prev;
      setSoundEnabled(next);
      if (next) {
        playTapChirp();
      }
      return next;
    });
  }, []);

  return { enabled, toggle };
}
