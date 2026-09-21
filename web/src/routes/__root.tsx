import * as React from "react";
import { createRootRoute, Outlet, useRouter } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/lib/query-client";
import { hydrateSession } from "@/lib/guards";
import { useSessionStore } from "@/stores/use-session-store";

export const Route = createRootRoute({
  beforeLoad: async () => {
    await hydrateSession();
  },
  component: RootComponent,
});

function RootComponent() {
  const state = useSessionStore((s) => s.state);
  const router = useRouter();

  React.useEffect(() => {
    // Infrastructure paths (the 401 interceptor, the global 403 resolution,
    // sign-out from the lock overlay) end the session without a navigation
    // attached; this effect is the single place that routes a dead session
    // to the login screen.
    if (state === "signed_out") {
      void router.navigate({ to: "/auth/login" });
    }
  }, [state, router]);

  return (
    <QueryClientProvider client={queryClient}>
      <Outlet />
    </QueryClientProvider>
  );
}
