import { redirect } from "@tanstack/react-router";
import { useSessionStore } from "@/stores/use-session-store";
import { fetchSessionState } from "@/features/auth/api/use-auth";

/**
 * Asks the server what the session is and records the answer.
 *
 * The token in localStorage is a cache, never the authority: only the server
 * knows whether a session is revoked, expired, or locked for inactivity.
 */
export async function hydrateSession(): Promise<void> {
  const store = useSessionStore.getState();
  if (!store.token) {
    store.clear();
    return;
  }
  try {
    store.applyServerState(await fetchSessionState());
  } catch {
    store.clear();
  }
}

export function requireAuthenticated(): void {
  const { state } = useSessionStore.getState();
  if (state === "signed_out") {
    throw redirect({ to: "/auth/login" });
  }
  // A locked session stays on the route; the lock overlay covers it.
}

export function requireCapability(capability: string): void {
  requireAuthenticated();
  const { state, capabilities } = useSessionStore.getState();
  // Capability is not evaluated while locked: the overlay is showing and the
  // operator has not yet proven they are still there.
  if (state === "authenticated" && !capabilities.includes(capability)) {
    // The miss target must be loop-free: `/` itself requires `sales.operate`,
    // so redirecting there would re-run this same failing guard forever.
    // `/no-access` is authentication-only, so the redirect always settles.
    throw redirect({ to: "/no-access" });
  }
}
