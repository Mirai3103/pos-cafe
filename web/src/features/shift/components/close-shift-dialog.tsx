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
  getReasonsForDimension,
  getDefaultReasonForDimension,
} from "@/features/shift/utils/discrepancy";
import type {
  ShiftCloseDiscrepancyInputDimension,
  ShiftCloseDiscrepancyInputReason,
} from "@/features/shift/utils/discrepancy";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";
import { messageForError } from "@/lib/error-messages";
import type {
  ShiftCurrentShiftResponse,
  ShiftClosingShiftResponse,
  ShiftCloseDiscrepancyInput,
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

  // Final IDs from latest attempts
  const cashCounts = recon?.cash_counts ?? [];
  const qrObservations = recon?.qr_observations ?? [];
  const latestCashCount = cashCounts[cashCounts.length - 1];
  const latestQR = qrObservations[qrObservations.length - 1];

  const hasCashDiscrepancy = nonZeroDims.some((d) => d.dimension === "CASH");
  const hasQRDiscrepancy = nonZeroDims.some(
    (d) => d.dimension === "MANUAL_QR_RECEIVED" || d.dimension === "MANUAL_QR_REFUNDED"
  );
  const needsCashRecount =
    hasCashDiscrepancy && (cashCounts.length < 2 || (latestCashCount?.sequence ?? 0) < 2);
  const needsQRRecheck =
    hasQRDiscrepancy && (qrObservations.length < 2 || (latestQR?.sequence ?? 0) < 2);

  const [reasons, setReasons] = useState<Record<string, ShiftCloseDiscrepancyInputReason>>(() => {
    const initial: Record<string, ShiftCloseDiscrepancyInputReason> = {};
    for (const d of nonZeroDims) {
      if (d.dimension) {
        initial[d.dimension] = getDefaultReasonForDimension(
          d.dimension as ShiftCloseDiscrepancyInputDimension
        );
      }
    }
    return initial;
  });

  const [notes, setNotes] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  if (!isOpen || !recon) return null;

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
    if (needsCashRecount) {
      playError();
      setError(
        "Ca có chênh lệch tiền mặt cần thực hiện 'Đếm lại tiền mặt' (tối thiểu 2 lần kiểm đếm) trước khi đóng ca."
      );
      return;
    }
    if (needsQRRecheck) {
      playError();
      setError(
        "Ca có chênh lệch VietQR cần thực hiện 'Cập nhật QR' (tối thiểu 2 lần quan sát) trước khi đóng ca."
      );
      return;
    }

    // Validate reason 'OTHER' note
    for (const d of nonZeroDims) {
      const dim = d.dimension as ShiftCloseDiscrepancyInputDimension;
      const reason = (dim && reasons[dim]) || getDefaultReasonForDimension(dim);
      if (reason === "OTHER" && !notes[dim]?.trim()) {
        playError();
        setError(
          `Vui lòng nhập ghi chú giải trình cho khoản lệch "${DIMENSION_LABELS[dim] || dim}" khi chọn "Lý do khác"`
        );
        return;
      }
    }

    try {
      let approverLoginCode: string | undefined;
      let managerPin: string | undefined;

      const discrepancies: ShiftCloseDiscrepancyInput[] = nonZeroDims
        .filter((d) => Boolean(d.dimension))
        .map((d) => {
          const dim = d.dimension as ShiftCloseDiscrepancyInputDimension;
          const reason = (dim && reasons[dim]) || getDefaultReasonForDimension(dim);
          // Backend rule: note is strictly forbidden unless reason is OTHER
          const note = reason === "OTHER" ? notes[dim]?.trim() || undefined : undefined;
          return {
            dimension: dim,
            reason,
            note,
          };
        });

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
      setError(messageForError(err));
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-lg overflow-hidden flex flex-col p-6 gap-4 max-h-[90vh] overflow-y-auto">
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

            {needsCashRecount && (
              <div className="p-3 rounded-xl border border-rose-200 bg-rose-50 text-rose-800 text-xs flex items-start gap-2">
                <AlertTriangle className="w-4 h-4 text-rose-600 shrink-0 mt-0.5" />
                <span>
                  <strong>Yêu cầu đếm lại tiền mặt:</strong> Hệ thống bắt buộc kiểm đếm tối thiểu 2 lần khi có chênh lệch tiền mặt. Vui lòng đóng bảng này và bấm <strong>&ldquo;Đếm lại tiền mặt&rdquo;</strong> trước.
                </span>
              </div>
            )}

            {needsQRRecheck && (
              <div className="p-3 rounded-xl border border-rose-200 bg-rose-50 text-rose-800 text-xs flex items-start gap-2">
                <AlertTriangle className="w-4 h-4 text-rose-600 shrink-0 mt-0.5" />
                <span>
                  <strong>Yêu cầu xác nhận VietQR lần 2:</strong> Hệ thống bắt buộc đối soát tối thiểu 2 lần khi có chênh lệch VietQR. Vui lòng đóng bảng này và bấm <strong>&ldquo;Cập nhật QR&rdquo;</strong> trước.
                </span>
              </div>
            )}

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
                const allowedReasons = getReasonsForDimension(
                  dim.dimension as ShiftCloseDiscrepancyInputDimension
                );

                return (
                  <div key={dimKey} className="p-3 rounded-xl border border-border bg-muted/20 space-y-2">
                    <div className="flex items-center justify-between text-xs font-semibold">
                      <span>{dimLabel}</span>
                      <span className="font-mono text-rose-600">{formatVND(diff)}</span>
                    </div>

                    <select
                      value={
                        reasons[dimKey] ||
                        getDefaultReasonForDimension(dim.dimension as ShiftCloseDiscrepancyInputDimension)
                      }
                      onChange={(e) =>
                        setReasons({
                          ...reasons,
                          [dimKey]: e.target.value as ShiftCloseDiscrepancyInputReason,
                        })
                      }
                      className="w-full h-9 px-2.5 rounded-lg border border-input bg-background text-xs font-medium"
                    >
                      {allowedReasons.map((k) => (
                        <option key={k} value={k}>
                          {REASON_LABELS[k]}
                        </option>
                      ))}
                    </select>

                    {reasons[dimKey] === "OTHER" ? (
                      <Input
                        value={notes[dimKey] || ""}
                        onChange={(e) => setNotes({ ...notes, [dimKey]: e.target.value })}
                        placeholder="Mô tả nguyên nhân chênh lệch (bắt buộc)..."
                        className="h-8 text-xs border-amber-300 focus-visible:ring-amber-500"
                        autoFocus
                      />
                    ) : (
                      <p className="text-[11px] text-muted-foreground italic px-1">
                        Không yêu cầu ghi chú cho lý do này
                      </p>
                    )}
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
            disabled={isPending || needsCashRecount || needsQRRecheck}
            className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
          >
            <Lock className="w-4 h-4 mr-1.5" />
            {isPending
              ? "Đang xử lý..."
              : hasDiscrepancy
              ? "Yêu cầu Quản lý duyệt đóng ca"
              : "Kết ca ngay"}
          </Button>
        </div>
      </div>
    </div>
  );
}

