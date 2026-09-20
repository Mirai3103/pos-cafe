import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { ApiError } from "./unwrap";
import { useSessionStore } from "@/stores/use-session-store";
import { fetchSessionState } from "@/features/auth/api/use-auth";

/**
 * A 403 is ambiguous: internal/auth/middleware.go returns it both for a locked
 * session and for a missing role. Only the server can tell them apart, so ask.
 */
async function resolveForbidden(error: unknown): Promise<void> {
  if (!(error instanceof ApiError) || error.status !== 403) return;
  try {
    const state = await fetchSessionState();
    if (state.state === "locked") {
      useSessionStore.getState().lock();
    } else if (state.state === "signed_out") {
      useSessionStore.getState().clear();
    }
  } catch {
    // Leave the session as it is; the next request will try again.
  }
}

export const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: (error) => void resolveForbidden(error) }),
  mutationCache: new MutationCache({ onError: (error) => void resolveForbidden(error) }),
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 2,
      retry: 1,
      refetchOnWindowFocus: false,
    },
    mutations: {
      retry: 0,
    },
  },
});
