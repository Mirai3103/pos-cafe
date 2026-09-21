import * as React from "react";
import { pingActivity } from "@/features/auth/api/use-auth";
import { useSessionStore } from "@/stores/use-session-store";

const THROTTLE_MS = 30_000;
const EVENTS = ["pointerdown", "keydown"] as const;

/**
 * Reports real human activity so the server can extend the inactivity window.
 *
 * The server owns the lock policy — 5 minutes for cashier and manager, 15 for
 * preparation. The client never decides to lock, and never reports activity
 * that a human did not cause.
 */
export function useActivityPing(): void {
  const state = useSessionStore((s) => s.state);
  const lastSent = React.useRef(0);

  React.useEffect(() => {
    if (state !== "authenticated") return;

    const onActivity = () => {
      const now = Date.now();
      if (now - lastSent.current < THROTTLE_MS) return;
      lastSent.current = now;
      void pingActivity().catch(() => {
        // A failed ping is not worth surfacing; the next interaction retries.
      });
    };

    for (const event of EVENTS) {
      window.addEventListener(event, onActivity, { passive: true });
    }
    return () => {
      for (const event of EVENTS) {
        window.removeEventListener(event, onActivity);
      }
    };
  }, [state]);
}
