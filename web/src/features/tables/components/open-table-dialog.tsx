import { useRef, useState, type ReactElement } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Coffee, X } from "lucide-react";
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4 backdrop-blur-xs">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="open-table-title"
        className="flex w-full max-w-md flex-col gap-4 rounded-3xl border border-border bg-card p-6 shadow-2xl"
      >
        <div className="flex items-center justify-between pb-3 border-b border-border">
          <div className="flex items-center gap-2.5">
            <div className="w-10 h-10 rounded-xl bg-emerald-50 dark:bg-emerald-950/60 text-emerald-700 dark:text-emerald-300 flex items-center justify-center border border-emerald-200/80">
              <Coffee className="w-5 h-5" />
            </div>
            <div>
              <h2 id="open-table-title" className="text-base font-bold text-foreground">
                Mở bàn
              </h2>
              <p className="text-xs text-muted-foreground">Chọn thêm bàn nếu khách ngồi ghép</p>
            </div>
          </div>
          <button
            type="button"
            aria-label="Đóng"
            onClick={onClose}
            disabled={isPending}
            className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-xl flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted transition"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="space-y-2">
          <label className="block text-xs font-bold text-muted-foreground uppercase tracking-wider">
            Bàn nhận khách:
          </label>
          <div className="grid grid-cols-3 gap-2">
            {choices.map((t) => {
              const isOn = selected.includes(t.id);
              return (
                <button
                  key={t.id}
                  type="button"
                  aria-pressed={isOn}
                  onClick={() => setSelected((ids) => toggleSelection(ids, t.id))}
                  className={`min-h-[48px] rounded-xl border px-3 text-sm font-semibold transition active:scale-[0.98] ${
                    isOn
                      ? "border-emerald-600 bg-emerald-50 dark:bg-emerald-950/60 text-emerald-900 dark:text-emerald-200 shadow-2xs font-bold"
                      : "border-border bg-card text-foreground hover:bg-muted"
                  }`}
                >
                  {t.name}
                </button>
              );
            })}
          </div>
        </div>

        {error && <p role="alert" className="text-xs font-semibold text-destructive">{error}</p>}

        <div className="pt-3 border-t border-border flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            disabled={isPending}
            className="min-h-[48px] h-12 px-4 rounded-xl border border-border bg-card hover:bg-muted text-xs font-bold text-foreground transition"
          >
            Hủy bỏ
          </button>
          <button
            type="button"
            onClick={handleConfirm}
            disabled={isPending || selected.length === 0}
            className="min-h-[48px] h-12 px-6 rounded-xl bg-emerald-600 hover:bg-emerald-700 active:scale-[0.98] text-xs font-bold text-white transition flex items-center gap-2 disabled:bg-muted disabled:text-muted-foreground disabled:cursor-not-allowed shadow-xs"
          >
            <Check className="w-4 h-4" />
            <span>{isPending ? "Đang mở bàn..." : "Mở bàn"}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
