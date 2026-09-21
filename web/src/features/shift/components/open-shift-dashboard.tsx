import { useState } from "react";
import { Clock, User, ArrowDownRight, ArrowUpRight, Lock, EyeOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { CashMovementDialog } from "./cash-movement-dialog";
import { DenominationCalculator } from "./denomination-calculator";
import {
  type DenominationCounts,
  calculateDenominationTotal,
} from "@/features/shift/utils/denomination";
import { useStartReconciliation } from "@/features/shift/api/use-shift";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";
import type { ShiftCurrentShiftResponse } from "@/api/generated/models";

interface OpenShiftDashboardProps {
  shift: ShiftCurrentShiftResponse;
}

export function OpenShiftDashboard({ shift: rawShift }: OpenShiftDashboardProps) {
  const shift = rawShift as unknown as {
    id: string;
    opened_at?: string;
    opener?: {
      display_name?: string;
    };
  };

  const { startReconciliation, isPending } = useStartReconciliation(shift.id);

  const [cashMovementOpen, setCashMovementOpen] = useState(false);
  const [movementMethod, setMovementMethod] = useState<"PAY_IN" | "PAY_OUT">("PAY_IN");
  const [showReconcileDialog, setShowReconcileDialog] = useState(false);

  const [counts, setCounts] = useState<DenominationCounts>({});
  const [reconcileError, setReconcileError] = useState<string | null>(null);

  const countedCash = calculateDenominationTotal(counts);

  const handleStartReconciliation = async () => {
    playClick();
    if (countedCash < 0) {
      playError();
      setReconcileError("Số tiền kiểm đếm không hợp lệ");
      return;
    }

    try {
      await startReconciliation({
        request_id: newRequestId(),
        counted_cash_vnd: countedCash,
      });
      playSuccess();
      setShowReconcileDialog(false);
    } catch (err: unknown) {
      playError();
      setReconcileError(err instanceof Error ? err.message : "Lỗi bắt đầu đối soát");
    }
  };

  const openCashMovement = (method: "PAY_IN" | "PAY_OUT") => {
    setMovementMethod(method);
    setCashMovementOpen(true);
  };

  const formattedOpenedAt = shift.opened_at
    ? new Date(shift.opened_at).toLocaleTimeString("vi-VN", {
        hour: "2-digit",
        minute: "2-digit",
        day: "2-digit",
        month: "2-digit",
      })
    : "—";

  return (
    <div className="space-y-6 max-w-5xl mx-auto p-4 sm:p-6 animate-in fade-in duration-200">
      {/* Top Banner Card */}
      <div className="p-6 rounded-2xl border border-border bg-card shadow-xs flex flex-col md:flex-row md:items-center justify-between gap-6">
        <div className="space-y-3">
          <div className="flex items-center gap-2.5">
            <Badge variant="default" className="bg-emerald-600 text-white font-bold px-3 py-1 text-xs">
              Ca đang hoạt động (OPEN)
            </Badge>
            <span className="text-xs font-mono text-muted-foreground">ID: {shift.id ? shift.id.slice(0, 8) : "—"}</span>
          </div>

          <div className="flex flex-wrap items-center gap-4 text-xs sm:text-sm text-muted-foreground">
            <div className="flex items-center gap-1.5">
              <User className="w-4 h-4 text-primary" />
              <span>Người mở ca: <strong className="text-foreground font-semibold">{shift.opener?.display_name ?? "—"}</strong></span>
            </div>
            <div className="flex items-center gap-1.5">
              <Clock className="w-4 h-4 text-primary" />
              <span>Bắt đầu lúc: <strong className="text-foreground font-semibold">{formattedOpenedAt}</strong></span>
            </div>
          </div>
        </div>

        {/* Quick Action Buttons */}
        <div className="flex flex-wrap items-center gap-3">
          <Button
            type="button"
            variant="outline"
            onClick={() => openCashMovement("PAY_IN")}
            className="min-h-12 h-12 rounded-xl text-xs font-bold border-emerald-200 bg-emerald-50/60 hover:bg-emerald-100 text-emerald-800"
          >
            <ArrowDownRight className="w-4 h-4 mr-1 text-emerald-600" />
            Nộp quỹ (Pay In)
          </Button>

          <Button
            type="button"
            variant="outline"
            onClick={() => openCashMovement("PAY_OUT")}
            className="min-h-12 h-12 rounded-xl text-xs font-bold border-rose-200 bg-rose-50/60 hover:bg-rose-100 text-rose-800"
          >
            <ArrowUpRight className="w-4 h-4 mr-1 text-rose-600" />
            Rút quỹ (Pay Out)
          </Button>

          <Button
            type="button"
            onClick={() => setShowReconcileDialog(true)}
            className="min-h-12 h-12 rounded-xl text-xs font-bold shadow-sm"
          >
            <Lock className="w-4 h-4 mr-1.5" />
            Kiểm tiền & Kết ca
          </Button>
        </div>
      </div>

      {/* Blind Count Boundary Notification */}
      <div className="p-5 rounded-2xl border border-border bg-muted/40 flex items-start gap-4">
        <div className="w-10 h-10 rounded-xl bg-muted text-muted-foreground flex items-center justify-center shrink-0">
          <EyeOff className="w-5 h-5" />
        </div>
        <div className="space-y-1 text-xs sm:text-sm">
          <h4 className="font-bold text-foreground">Nguyên tắc kiểm đếm mù (Blind Count Boundary)</h4>
          <p className="text-muted-foreground leading-relaxed">
            Trong suốt ca bán hàng, hệ thống không hiển thị số tiền mặt kỳ vọng hay doanh thu để đảm bảo tính minh bạch và trung thực. Khi bấm <strong>"Kiểm tiền & Kết ca"</strong>, bạn sẽ nhập số tiền mặt thực tế đang có trong két trước khi hệ thống đối soát.
          </p>
        </div>
      </div>

      {/* Cash Movement Dialog */}
      <CashMovementDialog
        shiftId={shift.id}
        isOpen={cashMovementOpen}
        onClose={() => setCashMovementOpen(false)}
        defaultMethod={movementMethod}
      />

      {/* Reconcile Prompt Modal */}
      {showReconcileDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
          <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-lg overflow-hidden flex flex-col p-6 gap-4">
            <div>
              <h3 className="text-base font-bold text-foreground">Bắt đầu kiểm tiền đối soát (Reconciliation)</h3>
              <p className="text-xs text-muted-foreground">
                Kiểm đếm toàn bộ số tiền mặt đang có trong ngăn kéo thu ngân
              </p>
            </div>

            <DenominationCalculator counts={counts} onChange={setCounts} />

            <div className="p-3 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-between">
              <span className="text-xs font-semibold text-primary">Tổng tiền kiểm đếm:</span>
              <span className="font-mono text-lg font-bold text-primary">{formatVND(countedCash)}</span>
            </div>

            {reconcileError && (
              <p className="text-xs text-destructive text-center font-medium">{reconcileError}</p>
            )}

            <div className="flex items-center gap-3 mt-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => setShowReconcileDialog(false)}
                className="w-1/2 h-12 rounded-xl"
              >
                Huỷ
              </Button>
              <Button
                type="button"
                onClick={handleStartReconciliation}
                disabled={isPending}
                className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
              >
                {isPending ? "Đang xử lý..." : "Xác nhận & Bắt đầu đối soát"}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
