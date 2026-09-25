import type { ReactElement } from "react";
import { AlertOctagon, CheckCircle2, Coffee, RefreshCw } from "lucide-react";
import type { AvailabilityStats as Stats } from "../lib/availability";

function StatCard({ label, value, icon: Icon }: { label: string; value: number; icon: typeof Coffee }) {
  return (
    <div className="flex items-center justify-between rounded-2xl border border-border bg-card p-4">
      <div>
        <span className="text-xs font-semibold text-muted-foreground">{label}</span>
        <div className="text-2xl font-bold font-mono text-foreground">{value}</div>
      </div>
      <Icon className="h-6 w-6 text-muted-foreground" />
    </div>
  );
}

export function AvailabilityStats({ stats, onRestore }: { stats: Stats; onRestore: () => void }): ReactElement {
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <StatCard label="Tổng danh mục" value={stats.total} icon={Coffee} />
      <StatCard label="Đang còn hàng" value={stats.available} icon={CheckCircle2} />
      <StatCard label="Tạm hết hàng" value={stats.unavailable} icon={AlertOctagon} />
      <div className="flex flex-col justify-center gap-2 rounded-2xl border border-border bg-card p-4">
        <span className="text-xs font-semibold text-muted-foreground">Thao tác nhanh</span>
        <button
          type="button"
          onClick={onRestore}
          disabled={stats.unavailable === 0}
          className="flex min-h-[48px] items-center justify-center gap-2 rounded-xl bg-primary px-3 text-xs font-bold text-primary-foreground transition disabled:cursor-not-allowed disabled:opacity-50"
        >
          <RefreshCw className="h-4 w-4" />
          <span>{`Khôi phục tất cả còn hàng (${stats.unavailable})`}</span>
        </button>
      </div>
    </div>
  );
}
