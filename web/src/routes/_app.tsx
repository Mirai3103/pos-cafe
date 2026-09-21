import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PosHeader } from "@/components/layout/pos-header";
import { LockOverlay } from "@/features/auth/components/lock-overlay";
import { useActivityPing } from "@/hooks/use-activity-ping";
import { requireAuthenticated } from "@/lib/guards";
import { useSessionStore } from "@/stores/use-session-store";

export const Route = createFileRoute("/_app")({
  beforeLoad: () => requireAuthenticated(),
  component: AppLayout,
});

function AppLayout() {
  const state = useSessionStore((s) => s.state);
  useActivityPing();

  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-background text-foreground">
      <PosHeader />
      <main className="flex-1 overflow-hidden">
        <Outlet />
      </main>
      {state === "locked" && <LockOverlay />}
    </div>
  );
}
