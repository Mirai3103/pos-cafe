import { useEffect, useState, type ReactElement } from "react";
import { AlertTriangle, Printer, Trash2, Undo2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
  onAdvance: (unitIds: string[], targetState: string) => void;
  onRequestWaste: (unit: BoardUnit) => void;
  onRequestCorrectState: (unit: BoardUnit) => void;
}

export function TicketCard({
  ticket,
  column,
  nowMs,
  busy,
  onAdvance,
  onRequestWaste,
  onRequestCorrectState,
}: TicketCardProps): ReactElement {
  const [selected, setSelected] = useState<Set<string>>(new Set());

  useEffect(() => {
    // oxlint-disable-next-line react/set-state-in-effect
    setSelected((previous) => {
      const unitIds = new Set(ticket.units.map((unit) => unit.id));
      const next = new Set([...previous].filter((id) => unitIds.has(id)));
      return next.size === previous.size ? previous : next;
    });
  }, [ticket]);

  const correctTarget = CORRECT_TARGET[column];
  const ageField = column === "QUEUED" ? "queuedAt" : "inPreparationAt";
  const oldestMinutes = ticket.units.reduce(
    (oldest, unit) => Math.max(oldest, minutesSince(unit[ageField], nowMs)),
    0,
  );

  function toggle(unitId: string) {
    setSelected((previous) => {
      const next = new Set(previous);
      if (next.has(unitId)) next.delete(unitId);
      else next.add(unitId);
      return next;
    });
  }

  const targetIds = selected.size > 0 ? Array.from(selected) : ticket.units.map((unit) => unit.id);
  const primaryLabel =
    selected.size > 0 ? `${PRIMARY_ACTION_LABEL[column]} (${selected.size})` : PRIMARY_ACTION_LABEL[column];

  return (
    <Card size="sm" className="border-t-4 border-t-accent">
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-sm font-bold">
          Đơn #{ticket.serviceNumber}
          {ticket.tableNames.length > 0 ? ` (${ticket.tableNames.join(", ")})` : ""}
        </CardTitle>
        <Badge variant="accent">{formatElapsed(oldestMinutes)}</Badge>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 text-sm">
        {ticket.units.map((unit) => (
          <div key={unit.id} className="flex items-start gap-2 border-b border-border pb-1.5 last:border-0 last:pb-0">
            <Checkbox
              checked={selected.has(unit.id)}
              onCheckedChange={() => toggle(unit.id)}
              className="mt-0.5"
              aria-label={`Chọn ${unit.itemName}`}
            />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="truncate font-medium">{unit.itemName}</span>
                {unit.sizeName && <span className="text-2xs text-muted-foreground">({unit.sizeName})</span>}
                {unit.isRemake && (
                  <Badge variant="destructive" className="text-[10px]">
                    PHA LẠI
                  </Badge>
                )}
              </div>
              {unit.modifierSummary && (
                <p className="truncate text-2xs text-muted-foreground">{unit.modifierSummary}</p>
              )}
              {unit.preparationNote && (
                <p className="truncate text-2xs text-amber-700">Ghi chú: {unit.preparationNote}</p>
              )}
            </div>
            <span className="shrink-0 font-mono text-2xs text-muted-foreground">#{unit.unitNumber}</span>
            {column !== "QUEUED" && (
              <button
                type="button"
                onClick={() => onRequestWaste(unit)}
                disabled={busy}
                title="Huỷ món (lỗi pha chế, không đạt, khách yêu cầu...)"
                className="shrink-0 rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
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
                className="shrink-0 rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
              >
                <Undo2 className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        ))}
      </CardContent>
      <CardFooter className="flex items-center gap-2">
        <Button
          type="button"
          disabled={busy}
          onClick={() => onAdvance(targetIds, NEXT_STATE[column])}
          className="h-12 flex-1 rounded-xl font-bold"
        >
          {primaryLabel}
        </Button>
        <button
          type="button"
          disabled
          title="Chưa hỗ trợ"
          aria-label="Báo thiếu nguyên liệu"
          className="cursor-not-allowed rounded-xl border border-border p-2.5 text-muted-foreground opacity-50"
        >
          <AlertTriangle className="h-4 w-4" />
        </button>
        <button
          type="button"
          disabled
          title="Chưa hỗ trợ"
          aria-label="In lại nhãn"
          className="cursor-not-allowed rounded-xl border border-border p-2.5 text-muted-foreground opacity-50"
        >
          <Printer className="h-4 w-4" />
        </button>
      </CardFooter>
    </Card>
  );
}
