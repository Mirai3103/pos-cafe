import { CheckCircle2, Flame, Hourglass } from "lucide-react";
import { COLUMN_LABELS, type BoardTicket, type BoardUnit, type ColumnKey } from "../lib/board";
import { TicketCard } from "./ticket-card";

export interface QueueColumnProps {
  column: ColumnKey;
  tickets: BoardTicket[];
  nowMs: number;
  /** True while a category filter is active, so the empty state blames the filter, not the queue. */
  filterActive?: boolean;
  busy: boolean;
  failedUnitIds: ReadonlySet<string>;
  onAdvance: (unitIds: string[], targetState: string) => void;
  onRequestWaste: (unit: BoardUnit) => void;
  onRequestCorrectState: (unit: BoardUnit) => void;
}

const COLUMN_HEADER_CONFIG = {
  QUEUED: {
    icon: Hourglass,
    iconWrapperClass: "bg-amber-100 text-amber-800",
    badgeClass: "bg-amber-50 text-amber-800 border-amber-200/70",
    subtitle: "Đơn mới từ quầy thu ngân",
    tagText: "Chờ chế biến",
    emptyIcon: Hourglass,
    emptyTitle: "Không có đơn chờ pha",
    emptySubtitle: "Đơn mới từ quầy thu ngân sẽ tự động xuất hiện tại đây.",
  },
  IN_PREPARATION: {
    icon: Flame,
    iconWrapperClass: "bg-emerald-100 text-emerald-800",
    badgeClass: "bg-emerald-50 text-emerald-800 border-emerald-200/70",
    subtitle: "Đang thao tác tại quầy",
    tagText: "Đang làm",
    emptyIcon: Flame,
    emptyTitle: "Không có đơn đang pha",
    emptySubtitle: 'Bấm "Bắt đầu làm" trên đơn chờ để chuyển vào cột này.',
  },
  READY: {
    icon: CheckCircle2,
    iconWrapperClass: "bg-indigo-100 text-indigo-800",
    badgeClass: "bg-indigo-50 text-indigo-800 border-indigo-200/70",
    subtitle: "Sẵn sàng gọi khách hoặc bưng bàn",
    tagText: "Chờ giao",
    emptyIcon: CheckCircle2,
    emptyTitle: "Chưa có đơn chờ giao",
    emptySubtitle: "Các đơn hoàn thành pha chế sẽ xuất hiện tại đây.",
  },
} as const;

export function QueueColumn({
  column,
  tickets,
  nowMs,
  filterActive = false,
  busy,
  failedUnitIds,
  onAdvance,
  onRequestWaste,
  onRequestCorrectState,
}: QueueColumnProps) {
  const unitCount = tickets.reduce((sum, t) => sum + t.units.length, 0);
  const config = COLUMN_HEADER_CONFIG[column];
  const Icon = config.icon;
  const EmptyIcon = config.emptyIcon;

  return (
    <section className="flex flex-col h-full min-h-0 bg-slate-100/80 rounded-2xl border border-slate-200/90 p-3 sm:p-4 shadow-2xs">
      {/* Column Header */}
      <div className="flex items-center justify-between pb-3 border-b border-slate-200/90 shrink-0">
        <div className="flex items-center gap-2.5">
          <div className={`w-7 h-7 rounded-lg ${config.iconWrapperClass} flex items-center justify-center font-bold shrink-0`}>
            <Icon className="w-4 h-4" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h2 className="font-bold text-sm sm:text-base text-slate-900 leading-none">{COLUMN_LABELS[column]}</h2>
              <span className={`font-mono text-xs font-bold px-2 py-0.5 rounded-full border ${config.badgeClass}`}>
                {unitCount}
              </span>
            </div>
            <p className="text-[11px] text-slate-500 mt-0.5">{config.subtitle}</p>
          </div>
        </div>
        <span className="hidden sm:inline-flex font-mono text-[10px] bg-white text-slate-500 px-2 py-1 rounded-md border border-slate-200 font-medium">
          {config.tagText}
        </span>
      </div>

      {/* Cards or Empty State */}
      <div className="flex-1 min-h-0 overflow-y-auto pt-3 pr-1 space-y-3.5">
        {tickets.length === 0 ? (
          <div className="h-48 rounded-2xl border-2 border-dashed border-slate-200 flex flex-col items-center justify-center p-6 text-center space-y-2 select-none my-auto">
            <div className="w-10 h-10 rounded-xl bg-slate-100 text-slate-400 flex items-center justify-center">
              <EmptyIcon className="w-5 h-5" />
            </div>
            <h4 className="text-xs font-bold text-slate-700">
              {filterActive ? "Không có món trong nhóm này" : config.emptyTitle}
            </h4>
            <p className="text-[11px] text-slate-400 max-w-[24ch] leading-relaxed">
              {filterActive ? "Thử chọn nhóm món khác hoặc chọn Tất cả." : config.emptySubtitle}
            </p>
          </div>
        ) : (
          tickets.map((ticket) => (
            <TicketCard
              key={ticket.serviceNumber}
              ticket={ticket}
              column={column}
              nowMs={nowMs}
              busy={busy}
              failedUnitIds={failedUnitIds}
              onAdvance={onAdvance}
              onRequestWaste={onRequestWaste}
              onRequestCorrectState={onRequestCorrectState}
            />
          ))
        )}
      </div>
    </section>
  );
}
