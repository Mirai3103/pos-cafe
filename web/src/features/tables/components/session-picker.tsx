import { X } from "lucide-react";
import type { FloorTable } from "../lib/floor";

export interface SessionPickerProps {
  table: FloorTable | null;
  canOpenNew: boolean;
  onPick: (sessionId: string) => void;
  onOpenNew: (table: FloorTable) => void;
  onClose: () => void;
}

/** A Table serving more than one party: choose which one the cashier means. */
export function SessionPicker({ table, canOpenNew, onPick, onOpenNew, onClose }: SessionPickerProps) {
  if (!table) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-xs">
      <div role="dialog" aria-modal="true" aria-labelledby="session-picker-title" className="flex w-full max-w-sm flex-col gap-3 rounded-2xl border border-border bg-card p-6 shadow-xl">
        <div className="flex items-start justify-between">
          <h2 id="session-picker-title" className="text-base font-bold">{table.name}</h2>
          <button type="button" aria-label="Đóng" onClick={onClose} className="h-12 w-12 rounded-xl flex items-center justify-center hover:bg-muted">
            <X className="h-4 w-4" />
          </button>
        </div>
        {table.occupants.map((o) => (
          <button key={o.sessionId} type="button" onClick={() => onPick(o.sessionId)} className="min-h-[48px] rounded-xl border border-border px-4 text-left font-mono text-sm font-bold hover:bg-muted">
            {`#${o.serviceNumber}`}
          </button>
        ))}
        {canOpenNew && table.available && (
          <button type="button" onClick={() => onOpenNew(table)} className="min-h-[48px] rounded-xl bg-primary px-4 text-sm font-bold text-primary-foreground">
            Mở phiên mới tại bàn này
          </button>
        )}
      </div>
    </div>
  );
}
