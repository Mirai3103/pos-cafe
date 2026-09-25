import { ArrowLeftRight, Utensils } from "lucide-react";
import type { SalesServiceSessionResponse } from "@/api/generated/models";
import { sessionTableLabel } from "../utils/dine-in";

export interface DineInHeaderProps {
  session: SalesServiceSessionResponse;
  onChangeTables: () => void;
}

export function DineInHeader({ session, onChangeTables }: DineInHeaderProps) {
  return (
    <div className="flex items-center justify-between gap-2 border-b border-border p-4 bg-muted/20 shrink-0">
      <div className="flex items-center gap-2.5 min-w-0">
        <div className="h-9 w-9 rounded-xl bg-primary/10 text-primary flex items-center justify-center shrink-0">
          <Utensils className="h-5 w-5" />
        </div>
        <div className="min-w-0">
          <p className="text-sm font-bold text-foreground truncate">
            {`${sessionTableLabel(session)} · #${session.service_number ?? ""}`}
          </p>
          <p className="text-2xs text-muted-foreground">Khách dùng tại bàn</p>
        </div>
      </div>
      <button type="button" onClick={onChangeTables} className="h-12 min-h-[48px] px-3 rounded-xl border border-border text-xs font-semibold flex items-center gap-1.5 hover:bg-muted shrink-0">
        <ArrowLeftRight className="h-4 w-4" /> Đổi bàn
      </button>
    </div>
  );
}
