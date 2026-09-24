import { MoreHorizontal, Pencil, Power } from "lucide-react";
import * as React from "react";
import type { FloorTable } from "../lib/floor";

export interface TableCardProps {
  table: FloorTable;
  canAdminister: boolean;
  onOpen: (table: FloorTable) => void;
  onRename: (table: FloorTable) => void;
  onToggleAvailability: (table: FloorTable) => void;
}

const STATE_CHIP: Record<FloorTable["state"], { label: string; tone: string }> = {
  FREE: { label: "Trống", tone: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300" },
  OCCUPIED: { label: "Có khách", tone: "bg-primary/10 text-primary" },
  UNAVAILABLE: { label: "Tạm ngưng", tone: "bg-muted text-muted-foreground" },
};

export function TableCard({ table, canAdminister, onOpen, onRename, onToggleAvailability }: TableCardProps) {
  const [isMenuOpen, setIsMenuOpen] = React.useState(false);
  const chip = STATE_CHIP[table.state];
  const switchedOff = !table.available && table.state === "OCCUPIED";

  return (
    <div className={`relative rounded-2xl border bg-card ${table.state === "UNAVAILABLE" ? "opacity-60" : ""}`}>
      <button
        type="button"
        onClick={() => onOpen(table)}
        disabled={table.state === "UNAVAILABLE"}
        className="w-full min-h-[120px] p-4 text-left flex flex-col justify-between gap-3 rounded-2xl hover:border-primary disabled:cursor-not-allowed select-none active:scale-[0.98] transition"
      >
        <div className="flex items-center justify-between gap-2 pr-10">
          <span className="text-base font-bold text-foreground">{table.name}</span>
          <span className={`rounded-md px-2 py-0.5 text-2xs font-bold ${chip.tone}`}>{chip.label}</span>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {table.occupants.map((o) => (
            <span key={o.sessionId} className="rounded-md bg-muted px-2 py-0.5 font-mono text-xs font-semibold">
              {`#${o.serviceNumber}`}
            </span>
          ))}
          {switchedOff && (
            <span className="rounded-md bg-amber-100 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300 px-2 py-0.5 text-2xs font-bold">
              Tạm ngưng
            </span>
          )}
        </div>
      </button>

      {canAdminister && (
        <div className="absolute top-2 right-2">
          <button
            type="button"
            aria-label={`Tùy chọn ${table.name}`}
            title={`Đổi tên / ${table.available ? "Tạm ngưng" : "Mở lại"}`}
            onClick={() => setIsMenuOpen((open) => !open)}
            className="h-12 w-12 min-h-[48px] rounded-xl flex items-center justify-center hover:bg-muted"
          >
            <MoreHorizontal className="h-4 w-4" />
          </button>
          {isMenuOpen && (
            <div role="menu" className="absolute right-0 z-10 mt-1 w-44 rounded-xl border border-border bg-card p-1 shadow-lg">
              <button
                type="button"
                role="menuitem"
                onClick={() => { setIsMenuOpen(false); onRename(table); }}
                className="w-full min-h-[48px] px-3 rounded-lg text-left text-sm flex items-center gap-2 hover:bg-muted"
              >
                <Pencil className="h-4 w-4" /> Đổi tên
              </button>
              <button
                type="button"
                role="menuitem"
                onClick={() => { setIsMenuOpen(false); onToggleAvailability(table); }}
                className="w-full min-h-[48px] px-3 rounded-lg text-left text-sm flex items-center gap-2 hover:bg-muted"
              >
                <Power className="h-4 w-4" /> {table.available ? "Tạm ngưng" : "Mở lại"}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
