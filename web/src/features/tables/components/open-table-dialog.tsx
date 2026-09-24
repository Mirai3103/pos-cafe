import { useRef, useState, type ReactElement } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { getGetTablesOverviewQueryKey } from "@/api/generated/endpoints/tables/tables";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { isConflictError } from "@/lib/unwrap";
import { useStartDineInSession } from "../api/use-tables";
import { toggleSelection, type FloorTable } from "../lib/floor";

export interface OpenTableDialogProps {
  initialTableId: string | null;
  tables: FloorTable[];
  onClose: () => void;
  onOpened: (sessionId: string) => void;
}

export function OpenTableDialog(props: OpenTableDialogProps): ReactElement | null {
  if (!props.initialTableId) return null;
  return <OpenTableForm key={props.initialTableId} {...props} initialTableId={props.initialTableId} />;
}

function OpenTableForm({
  initialTableId,
  tables,
  onClose,
  onOpened,
}: OpenTableDialogProps & { initialTableId: string }) {
  const queryClient = useQueryClient();
  const { startDineIn, isPending } = useStartDineInSession();
  const [selected, setSelected] = useState<string[]>([initialTableId]);
  const [error, setError] = useState<string | null>(null);
  // One seating is one intent: a retry after a lost response replays it.
  const requestIdRef = useRef(newRequestId());

  // Sharing a Table is legal, so the tapped one may already be occupied.
  const choices = tables.filter((t) => t.id === initialTableId || (t.available && t.state === "FREE"));

  async function handleConfirm() {
    if (isPending || selected.length === 0) return;
    setError(null);
    try {
      const session = await startDineIn(selected, requestIdRef.current);
      if (session.id) onOpened(session.id);
    } catch (err) {
      setError(messageForError(err));
      if (isConflictError(err)) {
        requestIdRef.current = newRequestId();
        void queryClient.invalidateQueries({ queryKey: getGetTablesOverviewQueryKey() });
      }
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-xs">
      <div role="dialog" aria-modal="true" aria-labelledby="open-table-title" className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-border bg-card p-6 shadow-xl">
        <div className="flex items-start justify-between">
          <div>
            <h2 id="open-table-title" className="text-base font-bold">Mở bàn</h2>
            <p className="text-xs text-muted-foreground">Chọn thêm bàn nếu khách ngồi ghép</p>
          </div>
          <button type="button" aria-label="Đóng" onClick={onClose} disabled={isPending} className="h-12 w-12 rounded-xl flex items-center justify-center hover:bg-muted">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="grid grid-cols-3 gap-2">
          {choices.map((t) => {
            const isOn = selected.includes(t.id);
            return (
              <button
                key={t.id}
                type="button"
                aria-pressed={isOn}
                onClick={() => setSelected((ids) => toggleSelection(ids, t.id))}
                className={`min-h-[48px] rounded-xl border px-3 text-sm font-semibold ${isOn ? "border-primary bg-primary/10 text-primary" : "border-border"}`}
              >
                {t.name}
              </button>
            );
          })}
        </div>
        {error && <p role="alert" className="text-xs font-semibold text-destructive">{error}</p>}
        <Button onClick={handleConfirm} disabled={isPending || selected.length === 0} className="h-12 min-h-[48px] rounded-xl font-bold">
          {isPending ? "Đang mở bàn..." : "Mở bàn"}
        </Button>
      </div>
    </div>
  );
}
