import { COLUMN_LABELS, type BoardTicket, type BoardUnit, type ColumnKey } from "../lib/board";
import { TicketCard } from "./ticket-card";

export interface QueueColumnProps {
  column: ColumnKey;
  tickets: BoardTicket[];
  nowMs: number;
  busy: boolean;
  onAdvance: (unitIds: string[], targetState: string) => void;
  onRequestWaste: (unit: BoardUnit) => void;
  onRequestCorrectState: (unit: BoardUnit) => void;
}

export function QueueColumn({ column, tickets, nowMs, busy, onAdvance, onRequestWaste, onRequestCorrectState }: QueueColumnProps) {
  const unitCount = tickets.reduce((sum, t) => sum + t.units.length, 0);

  return (
    <div className="flex flex-col gap-3 min-w-0 min-h-0">
      <div className="flex items-center justify-between px-1">
        <h2 className="font-bold text-sm text-foreground">{COLUMN_LABELS[column]}</h2>
        <span className="text-xs font-mono text-muted-foreground">{unitCount}</span>
      </div>
      <div className="flex flex-col gap-3 overflow-y-auto min-h-0">
        {tickets.length === 0 ? (
          <p className="text-xs text-muted-foreground text-center py-6">Không có đơn</p>
        ) : (
          tickets.map((ticket) => (
            <TicketCard
              key={ticket.serviceNumber}
              ticket={ticket}
              column={column}
              nowMs={nowMs}
              busy={busy}
              onAdvance={onAdvance}
              onRequestWaste={onRequestWaste}
              onRequestCorrectState={onRequestCorrectState}
            />
          ))
        )}
      </div>
    </div>
  );
}
