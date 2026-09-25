import type { ReactElement, ReactNode } from "react";
import { Link, Outlet } from "@tanstack/react-router";
import { useSessionStore } from "@/stores/use-session-store";
import { permittedTabs } from "../lib/tabs";

export interface SettingsLayoutProps {
  /** Rendered after a tab's label, keyed by the tab's route. */
  counters?: Partial<Record<string, ReactNode>>;
}

export function SettingsLayout({ counters = {} }: SettingsLayoutProps): ReactElement {
  const capabilities = useSessionStore((s) => s.capabilities);
  const tabs = permittedTabs(capabilities);

  return (
    <div className="flex h-full w-full flex-col bg-background">
      <nav role="tablist" aria-label="Cài đặt" className="flex items-center gap-2 border-b border-border bg-card px-4 py-2">
        {tabs.map((tab) => {
          const Icon = tab.icon;
          return (
            <Link
              key={tab.to}
              to={tab.to}
              role="tab"
              className="flex min-h-[48px] items-center gap-2 rounded-xl px-4 text-sm font-semibold text-muted-foreground transition hover:bg-muted hover:text-foreground"
              activeProps={{ className: "bg-primary text-primary-foreground hover:bg-primary hover:text-primary-foreground", "aria-selected": true }}
            >
              <Icon className="h-4 w-4" />
              <span>{tab.label}</span>
              {counters[tab.to]}
            </Link>
          );
        })}
      </nav>
      <main className="min-h-0 flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
