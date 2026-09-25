import { useState, type ReactElement } from "react";
import { Button } from "@/components/ui/button";
import { ErrorToast } from "@/components/feedback/error-toast";
import { messageForError } from "@/lib/error-messages";
import { playErrorBuzz, playTapChirp } from "@/lib/sound";
import { useAvailabilityMenu, useSetAvailability } from "../api/use-availability";
import { AvailabilityBoard } from "./availability-board";
import { RestoreAvailabilityDialog } from "./restore-availability-dialog";
import { EMPTY_FILTER, toAvailabilityView, type AvailabilityKind } from "../lib/availability";

export function AvailabilityView(): ReactElement {
  const menu = useAvailabilityMenu();
  const { setAvailability } = useSetAvailability();
  const [filter, setFilter] = useState(EMPTY_FILTER);
  const [error, setError] = useState<string | null>(null);
  const [restoreOpen, setRestoreOpen] = useState(false);

  if (menu.isPending) {
    return (
      <div className="grid grid-cols-1 gap-3 p-4 md:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 6 }, (_, i) => (
          <div key={i} className="h-32 animate-pulse rounded-2xl bg-muted" />
        ))}
      </div>
    );
  }

  if (menu.isError && !menu.data) {
    return (
      <div className="flex flex-col items-center gap-3 p-12 text-center">
        <p role="alert" className="text-sm font-semibold text-destructive">{messageForError(menu.error)}</p>
        <Button onClick={() => void menu.refetch()} className="h-12 min-h-[48px] rounded-xl px-6">Thử lại</Button>
      </div>
    );
  }

  const view = toAvailabilityView(menu.data);

  const handleToggle = (kind: AvailabilityKind, id: string, next: boolean) => {
    playTapChirp();
    setError(null);
    setAvailability(kind, id, next).catch((err: unknown) => {
      playErrorBuzz();
      setError(messageForError(err));
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
      <RestoreAvailabilityDialog open={restoreOpen} refs={view.unavailableRefs} onClose={() => setRestoreOpen(false)} />
      <ErrorToast message={error} onDismiss={() => setError(null)} />
    </>
  );
}
