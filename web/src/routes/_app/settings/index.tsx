import { createFileRoute, redirect } from "@tanstack/react-router";
import { firstPermittedTab } from "@/features/settings/lib/tabs";
import { useSessionStore } from "@/stores/use-session-store";

export const Route = createFileRoute("/_app/settings/")({
  beforeLoad: () => {
    const tab = firstPermittedTab(useSessionStore.getState().capabilities);
    throw redirect({ to: tab ? tab.to : "/no-access" });
  },
});
