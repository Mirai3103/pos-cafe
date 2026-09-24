import { Table2, Users, X } from "lucide-react";
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4 backdrop-blur-xs">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="session-picker-title"
        className="flex w-full max-w-sm flex-col gap-4 rounded-3xl border border-border bg-card p-6 shadow-2xl"
      >
        <div className="flex items-center justify-between pb-3 border-b border-border">
          <div className="flex items-center gap-2.5">
            <div className="w-10 h-10 rounded-xl bg-indigo-50 dark:bg-indigo-950/60 text-indigo-700 dark:text-indigo-300 flex items-center justify-center border border-indigo-200/80">
              <Table2 className="w-5 h-5" />
            </div>
            <div>
              <h2 id="session-picker-title" className="text-base font-bold text-foreground">
                {table.name}
              </h2>
              <p className="text-xs text-muted-foreground">Bàn đang phục vụ nhiều đơn ghép</p>
            </div>
          </div>
          <button
            type="button"
            aria-label="Đóng"
            onClick={onClose}
            className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-xl flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted transition"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="space-y-2">
          <label className="block text-xs font-bold text-muted-foreground uppercase tracking-wider">
            Chọn đơn cần thao tác:
          </label>
          <div className="space-y-2">
            {table.occupants.map((o) => (
              <button
                key={o.sessionId}
                type="button"
                onClick={() => onPick(o.sessionId)}
                className="w-full min-h-[48px] rounded-xl border border-border bg-card px-4 py-2 text-left font-mono text-sm font-bold text-foreground hover:border-indigo-300 hover:bg-indigo-50/20 active:scale-[0.98] transition flex items-center justify-between"
              >
                <span>{`#${o.serviceNumber}`}</span>
                <span className="text-xs font-sans font-medium text-muted-foreground">Vào đơn</span>
              </button>
            ))}
          </div>
        </div>

        {canOpenNew && table.available && (
          <div className="pt-2 border-t border-border">
            <button
              type="button"
              onClick={() => onOpenNew(table)}
              className="w-full min-h-[48px] rounded-xl bg-primary px-4 text-xs font-bold text-primary-foreground hover:bg-primary/90 active:scale-[0.98] transition flex items-center justify-center gap-2 shadow-xs"
            >
              <Users className="w-4 h-4" />
              <span>Mở phiên mới tại bàn này</span>
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
