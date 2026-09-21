import { useState } from "react";
import {
  Wallet,
  ArrowDownRight,
  ArrowUpRight,
  QrCode,
  RotateCcw,
  CheckCircle2,
  AlertTriangle,
  Lock,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { CashCountDialog } from "./cash-count-dialog";
import { QRObservationDialog } from "./qr-observation-dialog";
import { CloseShiftDialog } from "./close-shift-dialog";
import {
  DIMENSION_LABELS,
  type ShiftCloseDiscrepancyInputDimension,
} from "@/features/shift/utils/discrepancy";
import { formatVND } from "@/lib/utils";
import type {
  ShiftCurrentShiftResponse,
  ShiftClosingShiftResponse,
} from "@/api/generated/models";

interface ClosingReconciliationViewProps {
  shift: ShiftCurrentShiftResponse;
}

export function ClosingReconciliationView({ shift: rawShift }: ClosingReconciliationViewProps) {
  const shift = rawShift as unknown as ShiftClosingShiftResponse;
  const recon = shift.reconciliation;
  const [recountOpen, setRecountOpen] = useState(false);
  const [qrOpen, setQrOpen] = useState(false);
  const [closeDialogOpen, setCloseDialogOpen] = useState(false);

  if (!recon) return null;

  const canClose = recon.preview?.can_close ?? false;
  const shiftId = shift.id ?? "";
  const startedAt = recon.started_at
    ? new Date(recon.started_at).toLocaleTimeString("vi-VN")
    : "—";
  const starterName = recon.starter?.display_name ?? "—";
  const dimensions = recon.preview?.dimensions ?? [];

  const qrReceivedDim = dimensions.find((d) => d.dimension === "MANUAL_QR_RECEIVED");
  const qrRefundedDim = dimensions.find((d) => d.dimension === "MANUAL_QR_REFUNDED");
  const latestQrObs =
    recon.qr_observations && recon.qr_observations.length > 0
      ? recon.qr_observations[recon.qr_observations.length - 1]
      : undefined;
  const qrReceivedObserved =
    qrReceivedDim?.observed_vnd ?? latestQrObs?.observed_received_vnd ?? 0;
  const qrRefundedObserved =
    qrRefundedDim?.observed_vnd ?? latestQrObs?.observed_refunded_vnd ?? 0;

  return (
    <div className="space-y-6 max-w-5xl mx-auto p-4 sm:p-6 animate-in fade-in duration-200">
      {/* Header Bar */}
      <div className="p-6 rounded-2xl border border-border bg-card shadow-xs flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-1.5">
            <Badge variant="outline" className="border-amber-500 text-amber-600 bg-amber-50 font-bold">
              Đang đối soát kết ca (CLOSING)
            </Badge>
            <span className="text-xs font-mono text-muted-foreground">ID: {shiftId.slice(0, 8)}</span>
          </div>
          <p className="text-xs text-muted-foreground">
            Bắt đầu đối soát lúc: {startedAt} bởi {starterName}
          </p>
        </div>

        <div className="flex items-center gap-3">
          <Button
            type="button"
            onClick={() => setCloseDialogOpen(true)}
            className={`min-h-12 h-12 px-6 rounded-xl font-bold shadow-sm ${
              canClose
                ? "bg-emerald-600 hover:bg-emerald-700 text-white"
                : "bg-amber-600 hover:bg-amber-700 text-white"
            }`}
          >
            <Lock className="w-4 h-4 mr-2" />
            {canClose ? "Kết ca chính xác (Khớp 100%)" : "Kết ca có chênh lệch"}
          </Button>
        </div>
      </div>

      {/* Key Frozen Metrics */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs space-y-1">
          <span className="text-xs text-muted-foreground flex items-center gap-1.5">
            <Wallet className="w-3.5 h-3.5 text-primary" /> Tiền đầu ca
          </span>
          <p className="font-mono text-lg font-bold text-foreground">
            {formatVND(recon.opening_float_vnd ?? 0)}
          </p>
        </div>
        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs space-y-1">
          <span className="text-xs text-muted-foreground flex items-center gap-1.5">
            <ArrowDownRight className="w-3.5 h-3.5 text-emerald-600" /> Nộp thêm (Pay In)
          </span>
          <p className="font-mono text-lg font-bold text-emerald-600">
            +{formatVND(recon.pay_in_vnd ?? 0)}
          </p>
        </div>
        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs space-y-1">
          <span className="text-xs text-muted-foreground flex items-center gap-1.5">
            <ArrowUpRight className="w-3.5 h-3.5 text-rose-600" /> Rút chi (Pay Out)
          </span>
          <p className="font-mono text-lg font-bold text-rose-600">
            -{formatVND(recon.pay_out_vnd ?? 0)}
          </p>
        </div>
        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs space-y-1">
          <span className="text-xs text-muted-foreground flex items-center gap-1.5">
            <QrCode className="w-3.5 h-3.5 text-primary" /> Thu VietQR hệ thống
          </span>
          <p className="font-mono text-lg font-bold text-foreground">
            {formatVND(recon.expected_manual_qr_received_vnd ?? 0)}
          </p>
        </div>
      </div>

      {/* Comparison Dimensions Table */}
      <div className="border border-border rounded-2xl bg-card overflow-hidden shadow-xs">
        <div className="p-4 border-b border-border bg-muted/30 flex items-center justify-between">
          <h3 className="text-sm font-bold text-foreground">Bảng đối soát 3 chiều doanh thu & quỹ</h3>
          <div className="flex gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => setRecountOpen(true)} className="h-8 text-xs">
              <RotateCcw className="w-3.5 h-3.5 mr-1" /> Đếm lại tiền mặt
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => setQrOpen(true)} className="h-8 text-xs">
              <QrCode className="w-3.5 h-3.5 mr-1" /> Cập nhật QR
            </Button>
          </div>
        </div>

        <div className="divide-y divide-border/60">
          {dimensions.map((dim) => {
            const diff = dim.difference_vnd ?? 0;
            const isExact = diff === 0;
            const dimLabel =
              (dim.dimension &&
                DIMENSION_LABELS[dim.dimension as ShiftCloseDiscrepancyInputDimension]) ||
              dim.dimension ||
              "—";
            return (
              <div key={dim.dimension ?? "unknown"} className="p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-sm">
                <div className="space-y-0.5">
                  <span className="font-semibold text-foreground">{dimLabel}</span>
                  <div className="text-xs text-muted-foreground">
                    Kỳ vọng: <span className="font-mono">{formatVND(dim.expected_vnd ?? 0)}</span> • Thực tế: <span className="font-mono font-semibold text-foreground">{dim.observed_vnd !== null && dim.observed_vnd !== undefined ? formatVND(dim.observed_vnd) : "Chưa có"}</span>
                  </div>
                </div>

                <div className="flex items-center gap-3">
                  <div className="text-right">
                    <span className="text-xs text-muted-foreground block">Chênh lệch:</span>
                    <span className={`font-mono font-bold text-base ${isExact ? "text-emerald-600" : "text-rose-600"}`}>
                      {diff > 0 ? `+${formatVND(diff)}` : formatVND(diff)}
                    </span>
                  </div>

                  {isExact ? (
                    <Badge variant="default" className="bg-emerald-100 text-emerald-800 border-emerald-200">
                      <CheckCircle2 className="w-3.5 h-3.5 mr-1" /> Khớp
                    </Badge>
                  ) : (
                    <Badge variant="destructive" className="bg-rose-100 text-rose-800 border-rose-200">
                      <AlertTriangle className="w-3.5 h-3.5 mr-1" /> Lệch
                    </Badge>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Sub-modals */}
      {recountOpen && (
        <CashCountDialog
          shiftId={shiftId}
          isOpen={recountOpen}
          onClose={() => setRecountOpen(false)}
        />
      )}
      {qrOpen && (
        <QRObservationDialog
          shiftId={shiftId}
          isOpen={qrOpen}
          onClose={() => setQrOpen(false)}
          initialReceived={qrReceivedObserved}
          initialRefunded={qrRefundedObserved}
        />
      )}
      {closeDialogOpen && (
        <CloseShiftDialog
          shift={rawShift}
          isOpen={closeDialogOpen}
          onClose={() => setCloseDialogOpen(false)}
        />
      )}
    </div>
  );
}
