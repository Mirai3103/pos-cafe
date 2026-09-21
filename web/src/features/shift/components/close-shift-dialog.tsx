import { useState } from "react";
import { X, Lock, AlertTriangle, CheckCircle2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useCloseShift } from "@/features/shift/api/use-shift";
import { useManagerApprovalStore } from "@/stores/use-manager-approval-store";
import {
  DIMENSION_LABELS,
  REASON_LABELS,
  deriveNonZeroDimensions,
} from "@/features/shift/utils/discrepancy";
import type { ShiftCloseDiscrepancyInputDimension } from "@/features/shift/utils/discrepancy";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";
import type {
  ShiftCurrentShiftResponse,
  ShiftClosingShiftResponse,
  ShiftCloseDiscrepancyInput,
  ShiftCloseDiscrepancyInputReason,
} from "@/api/generated/models";

export interface CloseShiftDialogProps {
  shift: ShiftCurrentShiftResponse;
  isOpen: boolean;
  onClose: () => void;
}

export function CloseShiftDialog({ shift: rawShift, isOpen, onClose }: CloseShiftDialogProps) {
  const shift = rawShift as unknown as ShiftClosingShiftResponse;
  const shiftId = shift.id ?? "";
  const { closeShift, isPending } = useCloseShift(shiftId);
  const promptApproval = useManagerApprovalStore((s) => s.promptApproval);

  const recon = shift.reconciliation;
  const nonZeroDims = recon?.preview?.dimensions
    ? deriveNonZeroDimensions(recon.preview.dimensions)
    : [];
  const hasDiscrepancy = nonZeroDims.length > 0;

  const [reasons, setReasons] = useState<Record<string, ShiftCloseDiscrepancyInputReason>>(() => {
    const initial: Record<string, ShiftCloseDiscrepancyInputReason> = {};
    for (const d of nonZeroDims) {
      if (d.dimension) {
        initial[d.dimension] =
          d.dimension === "CASH" ? "CASH_COUNT_DIFFERENCE" : "QR_OBSERVATION_DIFFERENCE";
      }
    }
    return initial;
  });

  const [notes, setNotes] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  if (!isOpen || !recon) return null;

  // Final IDs from latest attempts
  const cashCounts = recon.cash_counts ?? [];
  const qrObservations = recon.qr_observations ?? [];
  const latestCashCount = cashCounts[cashCounts.length - 1];
  const latestQR = qrObservations[qrObservations.length - 1];

  const handleClose = async () => {
    playClick();

    if (!latestCashCount) {
      playError();
      setError("Cần ít nhất một lần kiểm đếm tiền mặt");
      return;
    }
    if (!latestQR) {
      playError();
      setError("Cần ít nhất một lần quan sát đối soát VietQR");
      return;
    }

    try {
      let approverLoginCode: string | undefined;
      let managerPin: string | undefined;

      const discrepancies: ShiftCloseDiscrepancyInput[] = nonZeroDims
        .filter((d) => Boolean(d.dimension))
        .map((d) => ({
          dimension: d.dimension as ShiftCloseDiscrepancyInputDimension,
          reason: (d.dimension && reasons[d.dimension]) || "UNEXPLAINED",
          note: d.dimension && notes[d.dimension]?.trim() ? notes[d.dimension].trim() : undefined,
        }));

      if (hasDiscrepancy) {
        // Collect Manager approval
        const creds = await promptApproval({
          title: "Duyệt Kết Ca Có Chênh Lệch",
          description: "Ca làm việc có chênh lệch tiền mặt hoặc VietQR. Cần Quản lý xác nhận đóng ca.",
          confirmLabel: "Xác nhận đóng ca lệch",
        });
        approverLoginCode = creds.approverLoginCode;
        managerPin = creds.managerPin;
      }

      await closeShift({
        request_id: newRequestId(),
        final_cash_count_id: latestCashCount.id,
        final_qr_observation_id: latestQR.id,
        discrepancies,
        approver_login_code: approverLoginCode,
        manager_pin: managerPin,
      });

      playSuccess();
      onClose();
    } catch (err: unknown) {
      playError();
      if (err instanceof Error && err.message === "MANAGER_APPROVAL_CANCELLED") {
        return;
      }
      setError(err instanceof Error ? err.message : "Lỗi kết ca làm việc");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-lg overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Xác nhận Kết thúc Ca làm việc</h3>
            <p className="text-xs text-muted-foreground">Đóng ca và chốt sổ bàn giao quầy thu ngân</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {hasDiscrepancy ? (
          <div className="space-y-4">
            <div className="p-3.5 rounded-xl border border-amber-200 bg-amber-50 text-amber-900 flex items-start gap-3">
              <AlertTriangle className="w-5 h-5 text-amber-600 shrink-0 mt-0.5" />
              <div className="text-xs space-y-1">
                <span className="font-bold block">Phát hiện chênh lệch số dư đối soát:</span>
                Vui lòng chọn lý do giải trình cho từng khoản chênh lệch. Quản lý cần nhập mã PIN phê duyệt để đóng ca.
              </div>
            </div>

            {/* List of discrepancies */}
            <div className="space-y-3">
              {nonZeroDims.map((dim) => {
                const diff = dim.difference_vnd ?? 0;
                const dimKey = dim.dimension ?? "";
                const dimLabel =
                  (dim.dimension &&
                    DIMENSION_LABELS[dim.dimension as ShiftCloseDiscrepancyInputDimension]) ||
                  dim.dimension ||
                  "—";
                return (
                  <div key={dimKey} className="p-3 rounded-xl border border-border bg-muted/20 space-y-2">
                    <div className="flex items-center justify-between text-xs font-semibold">
                      <span>{dimLabel}</span>
                      <span className="font-mono text-rose-600">{formatVND(diff)}</span>
                    </div>

                    <select
                      value={reasons[dimKey] || "UNEXPLAINED"}
                      onChange={(e) =>
                        setReasons({
                          ...reasons,
                          [dimKey]: e.target.value as ShiftCloseDiscrepancyInputReason,
                        })
                      }
                      className="w-full h-9 px-2.5 rounded-lg border border-input bg-background text-xs font-medium"
                    >
                      {Object.entries(REASON_LABELS).map(([k, v]) => (
                        <option key={k} value={k}>
                          {v}
                        </option>
                      ))}
                    </select>

                    <Input
                      value={notes[dimKey] || ""}
                      onChange={(e) => setNotes({ ...notes, [dimKey]: e.target.value })}
                      placeholder="Ghi chú thêm về chênh lệch..."
                      className="h-8 text-xs"
                    />
                  </div>
                );
              })}
            </div>
          </div>
        ) : (
          <div className="p-4 rounded-xl border border-emerald-200 bg-emerald-50 text-emerald-900 flex items-center gap-3">
            <CheckCircle2 className="w-6 h-6 text-emerald-600 shrink-0" />
            <div className="text-xs space-y-1">
              <span className="font-bold text-sm block">Số liệu khớp 100%!</span>
              Tiền mặt và VietQR hoàn toàn trùng khớp với hệ thống. Có thể kết ca ngay mà không cần Quản lý duyệt chênh lệch.
            </div>
          </div>
        )}

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        <div className="flex items-center gap-3 mt-2">
          <Button type="button" variant="outline" onClick={onClose} className="w-1/2 h-12 rounded-xl">
            Huỷ
          </Button>
          <Button
            type="button"
            onClick={handleClose}
            disabled={isPending}
            className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
          >
            <Lock className="w-4 h-4 mr-1.5" />
            {isPending ? "Đang xử lý..." : hasDiscrepancy ? "Yêu cầu Quản lý duyệt đóng ca" : "Kết ca ngay"}
          </Button>
        </div>
      </div>
    </div>
  );
}
