import type { ReactElement, ReactNode } from "react";
import { Link, Outlet, useNavigate } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useHotkeys } from "react-hotkeys-hook";
import { useSessionStore } from "@/stores/use-session-store";
import { PLANNED_TAB_HINT, permittedTabs, visiblePlannedTabs } from "../lib/tabs";

export interface SettingsLayoutProps {
  /** Rendered after a tab's label, keyed by the tab's route or planned-tab key. */
  counters?: Partial<Record<string, ReactNode>>;
}

const TAB_BASE =
  "flex h-12 min-h-[48px] shrink-0 items-center whitespace-nowrap gap-2.5 rounded-xl px-4 py-2 text-xs select-none transition sm:text-sm";

export function SettingsLayout({ counters = {} }: SettingsLayoutProps): ReactElement {
  const capabilities = useSessionStore((s) => s.capabilities);
  const navigate = useNavigate();
  const tabs = permittedTabs(capabilities);
  const planned = visiblePlannedTabs(capabilities);
  const canSell = capabilities.includes("sales.operate");

  useHotkeys(
    "f1",
    (event) => {
      event.preventDefault();
      void navigate({ to: "/" });
    },
    { enabled: canSell },
  );

  return (
    <div className="flex h-full w-full flex-col bg-slate-50 dark:bg-background">
      <div className="w-full shrink-0 border-b border-slate-200 bg-card px-4 py-2 sm:px-6 dark:border-border">
        <div className="mx-auto flex max-w-7xl items-center justify-between gap-3 overflow-x-auto">
          <nav role="tablist" aria-label="Cài đặt" className="flex items-center gap-2 sm:gap-3">
            {tabs.map((tab) => {
              const Icon = tab.icon;
              return (
                <Link
                  key={tab.to}
                  to={tab.to}
                  role="tab"
                  className={TAB_BASE}
                  activeProps={{
                    className: "bg-slate-900 font-bold text-white shadow-xs dark:bg-foreground dark:text-background",
                    "aria-selected": true,
                  }}
                  inactiveProps={{
                    className:
                      "border border-transparent font-semibold text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-muted-foreground dark:hover:bg-muted",
                    "aria-selected": false,
                  }}
                >
                  <Icon className="h-4 w-4" />
                  <span>{tab.label}</span>
                  {counters[tab.to]}
                </Link>
              );
            })}
            {planned.map((tab) => {
              const Icon = tab.icon;
              return (
                <button
                  key={tab.key}
                  type="button"
                  role="tab"
                  aria-selected={false}
                  aria-disabled
                  title={PLANNED_TAB_HINT}
                  className={`${TAB_BASE} cursor-not-allowed border border-transparent font-semibold text-slate-600 dark:text-muted-foreground`}
                >
                  <Icon className="h-4 w-4 text-slate-400" />
                  <span>{tab.label}</span>
                  {counters[tab.key]}
                </button>
              );
            })}
          </nav>

          {canSell && (
            <div className="hidden shrink-0 items-center gap-2 whitespace-nowrap md:flex">
              <Link
                to="/"
                className="flex h-12 min-h-[48px] items-center gap-2 rounded-xl border border-slate-200 bg-card px-3.5 text-xs font-semibold text-slate-700 shadow-2xs transition hover:bg-slate-50 active:scale-[0.98] dark:border-border dark:text-foreground dark:hover:bg-muted"
              >
                <ArrowLeft className="h-4 w-4 text-slate-500" />
                <span>Về Quầy thu ngân</span>
                <kbd className="rounded border border-slate-200 bg-slate-100 px-1.5 py-0.5 font-mono text-[10px] text-slate-500 dark:border-border dark:bg-muted">
                  F1
                </kbd>
              </Link>
            </div>
          )}
        </div>
      </div>
      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-7xl space-y-6 p-4 sm:p-6 lg:p-8">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
