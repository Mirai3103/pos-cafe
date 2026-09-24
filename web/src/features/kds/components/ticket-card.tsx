import { useEffect, useState, type ReactElement } from "react";
import {
  AlertCircle,
  AlertTriangle,
  Check,
  CheckCheck,
  Clock,
  Info,
  Play,
  Printer,
  ShoppingBag,
  Table2,
  Trash2,
  Undo2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";
import {
  CORRECT_TARGET,
  NEXT_STATE,
  PRIMARY_ACTION_LABEL,
  formatElapsed,
  minutesSince,
  type BoardTicket,
  type BoardUnit,
  type ColumnKey,
} from "../lib/board";

export interface TicketCardProps {
  ticket: BoardTicket;
  column: ColumnKey;
  nowMs: number;
  busy: boolean;
  failedUnitIds: ReadonlySet<string>;
  onAdvance: (unitIds: string[], targetState: string) => void;
  onRequestWaste: (unit: BoardUnit) => void;
  onRequestCorrectState: (unit: BoardUnit) => void;
}

// oxlint-disable-next-line react/only-export-components
export function resolveUnitSelection(selected: ReadonlySet<string>, units: BoardUnit[]) {
  const currentUnitIds = new Set(units.map((unit) => unit.id));
  const selectedUnitIds = [...selected].filter((id) => currentUnitIds.has(id));
  return {
    selectedUnitIds,
    targetUnitIds: selectedUnitIds.length > 0 ? selectedUnitIds : units.map((unit) => unit.id),
  };
}

export function TicketCard({
  ticket,
  column,
  nowMs,
  busy,
  failedUnitIds,
  onAdvance,
  onRequestWaste,
  onRequestCorrectState,
}: TicketCardProps): ReactElement {
  const [selected, setSelected] = useState<Set<string>>(new Set());

  useEffect(() => {
    // oxlint-disable-next-line react/set-state-in-effect
    setSelected((previous) => {
      const next = new Set(resolveUnitSelection(previous, ticket.units).selectedUnitIds);
      return next.size === previous.size ? previous : next;
    });
  }, [ticket]);

  const correctTarget = CORRECT_TARGET[column];
  const ageField = column === "QUEUED" ? "queuedAt" : "inPreparationAt";
  const oldestMinutes = ticket.units.reduce(
    (oldest, unit) => Math.max(oldest, minutesSince(unit[ageField], nowMs)),
    0,
  );

  const hasRemake = ticket.units.some((u) => u.isRemake);

  const timerClass =
    oldestMinutes < 3
      ? "text-emerald-700 bg-emerald-50 border-emerald-200"
      : oldestMinutes <= 5
        ? "text-amber-700 bg-amber-50 border-amber-200"
        : "text-rose-700 bg-rose-50 border-rose-300 animate-pulse font-bold";

  const earliestQueuedAt = ticket.units.reduce<string | null>((earliest, u) => {
    if (!u.queuedAt) return earliest;
    if (!earliest) return u.queuedAt;
    return new Date(u.queuedAt) < new Date(earliest) ? u.queuedAt : earliest;
  }, null);
  const formattedTime = earliestQueuedAt
    ? new Date(earliestQueuedAt).toLocaleTimeString("vi-VN", { hour: "2-digit", minute: "2-digit" })
    : "";

  function toggle(unitId: string) {
    setSelected((previous) => {
      const next = new Set(previous);
      if (next.has(unitId)) next.delete(unitId);
      else next.add(unitId);
      return next;
    });
  }

  const { selectedUnitIds, targetUnitIds } = resolveUnitSelection(selected, ticket.units);
  const primaryLabel =
    selectedUnitIds.length > 0
      ? `${PRIMARY_ACTION_LABEL[column]} (${selectedUnitIds.length})`
      : PRIMARY_ACTION_LABEL[column];

  return (
    <div
      className={cn(
        "bg-white rounded-2xl border p-4 space-y-3 transition duration-200 shadow-card hover:shadow-card-hover",
        hasRemake ? "border-rose-300 ring-2 ring-rose-200/60" : "border-slate-200",
      )}
    >
      {/* Card Header Row */}
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-mono text-base font-bold text-slate-900 tracking-tight">
            Đơn #{ticket.serviceNumber}
          </span>
          {ticket.tableNames.length > 0 ? (
            <span className="text-indigo-700 bg-indigo-50 border border-indigo-200 px-2.5 py-1 rounded-lg text-xs font-semibold flex items-center gap-1.5">
              <Table2 className="w-3.5 h-3.5" />
              {ticket.tableNames.join(", ")}
            </span>
          ) : (
            <span className="text-emerald-700 bg-emerald-50 border border-emerald-200 px-2.5 py-1 rounded-lg text-xs font-semibold flex items-center gap-1.5">
              <ShoppingBag className="w-3.5 h-3.5" />
              Mang đi
            </span>
          )}
          {hasRemake && (
            <span className="bg-rose-100 text-rose-800 border border-rose-300 px-2 py-0.5 rounded text-[11px] font-bold uppercase tracking-wide inline-flex items-center gap-1">
              <AlertCircle className="w-3.5 h-3.5" />
              PHA LẠI
            </span>
          )}
        </div>
        <div className={cn("font-mono text-xs font-bold px-2.5 py-1 rounded-lg flex items-center gap-1.5 shadow-2xs border", timerClass)}>
          <Clock className="w-3.5 h-3.5" />
          <span>{formatElapsed(oldestMinutes)}</span>
        </div>
      </div>

      {/* Subtitle Info Row */}
      <div className="flex items-center justify-between text-[11px] text-slate-400">
        <span>Đơn #{ticket.serviceNumber}</span>
        {formattedTime && <span className="font-mono">{formattedTime}</span>}
      </div>

      {/* Checklist of items */}
      <div className="border-t border-b border-slate-100 py-2 space-y-2">
        {ticket.units.map((unit) => {
          const failed = failedUnitIds.has(unit.id);
          const isSelected = selected.has(unit.id);
          return (
            <div key={unit.id} className="py-1.5 flex items-start gap-2.5">
              <Checkbox
                checked={isSelected}
                onCheckedChange={() => toggle(unit.id)}
                className="mt-0.5"
                aria-label={`Chọn ${unit.itemName} #${unit.unitNumber}`}
              />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-1.5 flex-wrap">
                  <span className="font-mono font-bold text-xs bg-slate-100 text-slate-800 px-1.5 py-0.5 rounded border border-slate-200">
                    #{unit.unitNumber}
                  </span>
                  <span className={cn("font-semibold text-sm leading-snug", isSelected ? "text-slate-900 font-bold" : "text-slate-900")}>
                    {unit.itemName}
                  </span>
                  {unit.sizeName && <span className="text-xs font-normal text-slate-500">({unit.sizeName})</span>}
                  {unit.isRemake && (
                    <span className="bg-rose-100 text-rose-800 border border-rose-300 px-1.5 py-0.5 rounded text-[10px] font-bold">
                      PHA LẠI
                    </span>
                  )}
                </div>
                {unit.modifierSummary && (
                  <div className="flex flex-wrap gap-1 mt-1">
                    <span className="text-[11px] font-medium text-slate-600 bg-slate-100 px-1.5 py-0.5 rounded border border-slate-200/60">
                      {unit.modifierSummary}
                    </span>
                  </div>
                )}
                {unit.preparationNote && (
                  <div className="mt-1">
                    <span className="text-[11px] font-semibold text-amber-900 bg-amber-50 border border-amber-200/80 px-2 py-0.5 rounded-md inline-flex items-center gap-1">
                      <Info className="h-3 w-3 text-amber-600 shrink-0" />
                      {unit.preparationNote}
                    </span>
                  </div>
                )}
                {failed && <p className="text-2xs font-semibold text-destructive mt-0.5">Không thể chuyển trạng thái</p>}
              </div>
              {column !== "QUEUED" && (
                <button
                  type="button"
                  onClick={() => onRequestWaste(unit)}
                  disabled={busy}
                  title="Huỷ món (lỗi pha chế, không đạt, khách yêu cầu...)"
                  aria-label="Huỷ món"
                  className="h-8 w-8 rounded-xl border border-slate-200 bg-slate-50 hover:bg-rose-50 hover:border-rose-200 hover:text-rose-600 text-slate-400 flex items-center justify-center transition disabled:opacity-50 shrink-0 cursor-pointer"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              )}
              {correctTarget && (
                <button
                  type="button"
                  onClick={() => onRequestCorrectState(unit)}
                  disabled={busy}
                  title="Hoàn tác thao tác gần nhất (cần Quản lý duyệt)"
                  aria-label="Hoàn tác thao tác"
                  className="h-8 w-8 rounded-xl border border-slate-200 bg-slate-50 hover:bg-slate-100 hover:text-slate-800 text-slate-400 flex items-center justify-center transition disabled:opacity-50 shrink-0 cursor-pointer"
                >
                  <Undo2 className="h-3.5 w-3.5" />
                </button>
              )}
            </div>
          );
        })}
      </div>

      {/* Action Buttons Row */}
      <div className="pt-2 flex items-center gap-2">
        <Button
          type="button"
          disabled={busy}
          onClick={() => onAdvance(targetUnitIds, NEXT_STATE[column])}
          className={cn(
            "min-h-[48px] h-12 flex-1 rounded-xl text-white font-bold text-xs sm:text-sm shadow-xs flex items-center justify-center gap-2 transition cursor-pointer",
            column === "READY"
              ? "bg-slate-900 hover:bg-slate-800 active:scale-[0.98]"
              : "bg-emerald-600 hover:bg-emerald-700 active:scale-[0.98]",
          )}
        >
          {column === "QUEUED" && <Play className="w-4 h-4" />}
          {column === "IN_PREPARATION" && <Check className="w-4 h-4" />}
          {column === "READY" && <CheckCheck className="w-4 h-4" />}
          <span>{primaryLabel}</span>
        </Button>
        <button
          type="button"
          disabled
          title="Chưa hỗ trợ"
          aria-label="Báo thiếu nguyên liệu"
          className="min-h-[48px] min-w-[48px] h-12 px-3 rounded-xl border border-slate-200 bg-slate-50 text-slate-400 opacity-50 cursor-not-allowed flex items-center justify-center gap-1.5 shrink-0"
        >
          <AlertTriangle className="w-4 h-4 text-amber-600" />
          <span className="hidden sm:inline text-xs font-semibold text-slate-500">Báo thiếu</span>
        </button>
        <button
          type="button"
          disabled
          title="Chưa hỗ trợ"
          aria-label="In lại nhãn"
          className="min-h-[48px] min-w-[48px] h-12 w-12 rounded-xl border border-slate-200 bg-slate-50 text-slate-400 opacity-50 cursor-not-allowed flex items-center justify-center shrink-0"
        >
          <Printer className="w-4 h-4 text-slate-500" />
        </button>
      </div>
    </div>
  );
}
