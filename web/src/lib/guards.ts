import { redirect } from "@tanstack/react-router";
import { ApiError } from "@/lib/unwrap";
import { useSessionStore } from "@/stores/use-session-store";
import { fetchSessionState } from "@/features/auth/api/use-auth";

let hydration: Promise<void> | null = null;

/**
 * Asks the server what the session is and records the answer, once per page
 * load. Later navigations await the settled promise instead of re-fetching:
 * mid-session changes arrive through the sign-in/unlock seam and the global
 * 403 resolution, not through hydration.
 */
export function hydrateSession(): Promise<void> {
  if (!hydration) {
    hydration = runHydration();
  }
  return hydration;
}

async function runHydration(): Promise<void> {
  const store = useSessionStore.getState();
  if (!store.token) {
    store.clear();
    return;
  }
  try {
    store.applyServerState(await fetchSessionState());
  } catch (error) {
    // Only an authoritative failure ends the session. The server reports a
    // revoked or expired token as signed_out with HTTP 200, so a 401 here
    // means the same thing. A network error or 5xx is an outage: the cached
    // session stays and the next real request surfaces the problem.
    if (error instanceof ApiError && error.status === 401) {
      store.clear();
    }
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
