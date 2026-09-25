import { useCallback, useState, type ReactElement } from "react";
import { Button } from "@/components/ui/button";
import { messageForError } from "@/lib/error-messages";
import { playErrorBuzz, playTapChirp } from "@/lib/sound";
import { useAvailabilityMenu, useSetAvailability } from "../api/use-availability";
import { AvailabilityBoard } from "./availability-board";
import { RestoreAvailabilityDialog } from "./restore-availability-dialog";
import { StockToast, type StockToastMessage } from "./stock-toast";
import { EMPTY_FILTER, toAvailabilityView, type AvailabilityKind } from "../lib/availability";
import { RESTORE_SUCCESS_MESSAGE, toggleSuccessMessage } from "../lib/stock-messages";

export function AvailabilityView(): ReactElement {
  const menu = useAvailabilityMenu();
  const { setAvailability } = useSetAvailability();
  const [filter, setFilter] = useState(EMPTY_FILTER);
  const [toast, setToast] = useState<StockToastMessage | null>(null);
  const [restoreOpen, setRestoreOpen] = useState(false);
  const dismissToast = useCallback(() => setToast(null), []);

  if (menu.isPending) {
    return (
      <div className="space-y-5">
        <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 4 }, (_, i) => (
            <div key={i} className="h-[114px] animate-pulse rounded-2xl bg-slate-200/60 dark:bg-muted" />
          ))}
        </div>
        <div className="h-[150px] animate-pulse rounded-2xl bg-slate-200/60 dark:bg-muted" />
        <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 6 }, (_, i) => (
            <div key={i} className="h-[114px] animate-pulse rounded-2xl bg-slate-200/60 dark:bg-muted" />
          ))}
        </div>
      </div>
    );
  }

  if (menu.isError && !menu.data) {
    return (
      <div className="flex flex-col items-center gap-3 rounded-2xl border border-slate-200 bg-card p-12 text-center dark:border-border">
        <p role="alert" className="text-sm font-semibold text-destructive">{messageForError(menu.error)}</p>
        <Button onClick={() => void menu.refetch()} className="h-12 min-h-[48px] rounded-xl px-6">Thử lại</Button>
      </div>
    );
  }

  const view = toAvailabilityView(menu.data);

  const handleToggle = (kind: AvailabilityKind, id: string, next: boolean, name: string) => {
    playTapChirp();
    setToast(null);
    setAvailability(kind, id, next)
      .then(() => setToast({ text: toggleSuccessMessage(name, next), tone: "success" }))
      .catch((err: unknown) => {
        playErrorBuzz();
        setToast({ text: messageForError(err), tone: "error" });
      });
  };

  return (
    <>
      <AvailabilityBoard
        view={view}
        filter={filter}
        onFilterChange={setFilter}
        onToggle={handleToggle}
        onRestore={() => setRestoreOpen(true)}
      />
      <RestoreAvailabilityDialog
        open={restoreOpen}
        refs={view.unavailableRefs}
        onClose={() => setRestoreOpen(false)}
        onRestored={() => setToast({ text: RESTORE_SUCCESS_MESSAGE, tone: "success" })}
      />
      <StockToast toast={toast} onDismiss={dismissToast} />
    </>
  );
}
