import type { ReactElement } from "react";
import { AlertTriangle, Check } from "lucide-react";
import type { PreparationQueueAlertResponse } from "@/api/generated/models";
import { Button } from "@/components/ui/button";

const ALERT_KIND_LABELS: Record<string, string> = {
  CANCELLATION: "Đã huỷ",
  CHANGE: "Đã đổi món",
  WASTE: "Đã huỷ do lỗi/hết",
};

const ALERT_REASON_LABELS: Record<string, string> = {
  CUSTOMER_REQUEST: "Khách yêu cầu",
  ITEM_UNAVAILABLE: "Món không có sẵn",
  ORDER_ENTRY_ERROR: "Lỗi nhập đơn",
  PREPARATION_ERROR: "Lỗi pha chế",
  QUALITY_FAILURE: "Không đạt chất lượng",
  OTHER: "Lý do khác",
};

export interface AlertsPanelProps {
  alerts: PreparationQueueAlertResponse[];
  busy: boolean;
  onAcknowledge: (alertId: string) => void;
}

export function AlertsPanel({ alerts, busy, onAcknowledge }: AlertsPanelProps): ReactElement | null {
  if (alerts.length === 0) return null;

  return (
    <div className="flex flex-col gap-2 rounded-2xl border border-amber-300 bg-amber-50 p-3">
      {alerts.map((alert) => (
        <div key={alert.id} className="flex items-center gap-3 rounded-xl border border-amber-200 bg-white p-2.5">
          <AlertTriangle className="h-4 w-4 shrink-0 text-amber-600" />
          <div className="min-w-0 flex-1 text-xs">
            <span className="font-bold">{ALERT_KIND_LABELS[alert.kind ?? ""] ?? "Cảnh báo chuẩn bị"}</span>
            {" — "}
            <span>{`${alert.item_name} #${alert.unit_number} (Đơn #${alert.service_number})`}</span>
            {alert.reason && (
              <span className="text-muted-foreground">
                {" · "}
                {ALERT_REASON_LABELS[alert.reason] ?? "Lý do khác"}
              </span>
            )}
          </div>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={() => alert.id && onAcknowledge(alert.id)}
          >
            <Check className="mr-1 h-3.5 w-3.5" />
            Đã biết
          </Button>
        </div>
      ))}
    </div>
  );
}
