import { usePreferencesStore } from "@/stores/use-preferences-store";

let sharedAudioCtx: AudioContext | null = null;

export function getSoundEnabled(): boolean {
  return usePreferencesStore.getState().soundEnabled;
}

export function setSoundEnabled(enabled: boolean): void {
  usePreferencesStore.getState().setSoundEnabled(enabled);
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

/**
 * New order on the Preparation Queue: two-tone bell (659Hz -> 880Hz) ported
 * from design-system/pos-cafe/pages/kds.html. Gated by its own preference, not
 * by tap feedback, so muting key clicks never silences the kitchen.
 */
export function playNewOrderChime(): void {
  if (!usePreferencesStore.getState().newOrderChime) return;
  try {
    const ctx = getAudioContext();
    if (!ctx) return;
    const now = ctx.currentTime;
    [659, 880].forEach((freq, idx) => {
      const at = now + idx * 0.18;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = "sine";
      osc.frequency.setValueAtTime(freq, at);
      gain.gain.setValueAtTime(0, at);
      gain.gain.linearRampToValueAtTime(0.2, at + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.001, at + 0.35);
      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.start(at);
      osc.stop(at + 0.35);
    });
  } catch {
    // Ignore sound playback errors
  }
}

/** React hook for listening and toggling sound feedback state */
export function useSound() {
  const enabled = usePreferencesStore((s) => s.soundEnabled);

  const toggle = () => {
    setSoundEnabled(!enabled);
    if (!enabled) playTapChirp();
  };

  return { enabled, toggle };
}

/** Semantic aliases matching action / prompt naming */
export const playClick = playTapChirp;
export const playAction = playSuccessChirp;
export const playSuccess = playSuccessChirp;
export const playError = playErrorBuzz;
