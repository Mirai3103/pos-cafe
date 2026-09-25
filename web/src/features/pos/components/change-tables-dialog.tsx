import { useRef, useState, type ReactElement } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { getGetTablesOverviewQueryKey } from "@/api/generated/endpoints/tables/tables";
import type { SalesServiceSessionResponse } from "@/api/generated/models";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { isConflictError } from "@/lib/unwrap";
import { useSetSessionTables, useTablesOverview } from "@/features/tables/api/use-tables";
import { toFloorTables, toggleSelection, type FloorTable } from "@/features/tables/lib/floor";

export interface ChangeTablesChoicesProps {
  tables: FloorTable[];
  /** This Session; its own occupancy is not "another party". */
  sessionId: string;
  currentIds: string[];
  selected: string[];
  onToggle: (id: string) => void;
}

/** Held Tables plus every available one; sharing a Table with another party is legal. */
export function ChangeTablesChoices({ tables, sessionId, currentIds, selected, onToggle }: ChangeTablesChoicesProps) {
  const choices = tables.filter((t) => currentIds.includes(t.id) || t.available);
  return (
    <div className="grid grid-cols-2 gap-2">
      {choices.map((t) => {
        const isOn = selected.includes(t.id);
        const others = t.occupants.filter((o) => o.sessionId !== sessionId);
        return (
          <button
            key={t.id}
            type="button"
            aria-pressed={isOn}
            onClick={() => onToggle(t.id)}
            className={`min-h-[48px] rounded-xl border px-3 py-2 text-left ${isOn ? "border-primary bg-primary/10" : "border-border"}`}
          >
            <span className="block text-sm font-semibold">{t.name}</span>
            {others.length > 0 && (
              <span className="block font-mono text-2xs text-muted-foreground">
                {others.map((o) => `#${o.serviceNumber}`).join(" ")}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

export interface ChangeTablesDialogProps {
  session: SalesServiceSessionResponse | null;
  onClose: () => void;
}

export function ChangeTablesDialog({ session, onClose }: ChangeTablesDialogProps): ReactElement | null {
  if (!session?.id) return null;
  return <ChangeTablesForm key={session.id} session={session} onClose={onClose} />;
}

function ChangeTablesForm({ session, onClose }: { session: SalesServiceSessionResponse; onClose: () => void }) {
  const queryClient = useQueryClient();
  const overview = useTablesOverview();
  const { setTables, isPending } = useSetSessionTables();
  const currentIds = (session.tables ?? []).map((t) => t.id ?? "").filter(Boolean);
  const [selected, setSelected] = useState<string[]>(currentIds);
  const [error, setError] = useState<string | null>(null);
  const requestIdRef = useRef(newRequestId());

  async function handleSave() {
    if (isPending || selected.length === 0 || !session.id) return;
    setError(null);
    try {
      await setTables(session.id, selected, requestIdRef.current);
      onClose();
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
      <div role="dialog" aria-modal="true" aria-labelledby="change-tables-title" className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-border bg-card p-6 shadow-xl">
        <div className="flex items-start justify-between">
          <div>
            <h2 id="change-tables-title" className="text-base font-bold">Đổi bàn</h2>
            <p className="text-xs text-muted-foreground">Chọn các bàn khách đang ngồi. Cần ít nhất một bàn.</p>
          </div>
          <button type="button" aria-label="Đóng" onClick={onClose} disabled={isPending} className="h-12 w-12 rounded-xl flex items-center justify-center hover:bg-muted">
            <X className="h-4 w-4" />
          </button>
        </div>
        <ChangeTablesChoices
          tables={toFloorTables(overview.data)}
          sessionId={session.id ?? ""}
          currentIds={currentIds}
          selected={selected}
          onToggle={(id) => setSelected((ids) => toggleSelection(ids, id))}
        />
        {error && <p role="alert" className="text-xs font-semibold text-destructive">{error}</p>}
        <Button onClick={handleSave} disabled={isPending || selected.length === 0} className="h-12 min-h-[48px] rounded-xl font-bold">
          {isPending ? "Đang lưu..." : "Lưu"}
        </Button>
      </div>
    </div>
  );
}
