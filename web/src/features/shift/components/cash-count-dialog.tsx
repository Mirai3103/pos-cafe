import { useState } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DenominationCalculator } from "./denomination-calculator";
import {
  type DenominationCounts,
  calculateDenominationTotal,
} from "@/features/shift/utils/denomination";
import { useRecordCashCount } from "@/features/shift/api/use-shift";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";

interface CashCountDialogProps {
  shiftId: string;
  isOpen: boolean;
  onClose: () => void;
}

export function CashCountDialog({ shiftId, isOpen, onClose }: CashCountDialogProps) {
  const { recordCashCount, isPending } = useRecordCashCount(shiftId);
  const [counts, setCounts] = useState<DenominationCounts>({});
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const countedCash = calculateDenominationTotal(counts);

  const handleSubmit = async () => {
    playClick();
    if (countedCash < 0) {
      playError();
      setError("Số tiền không hợp lệ");
      return;
    }

    try {
      await recordCashCount({
        request_id: newRequestId(),
        counted_cash_vnd: countedCash,
      });
      playSuccess();
      onClose();
    } catch (err: unknown) {
      playError();
      setError(err instanceof Error ? err.message : "Không thể ghi nhận kiểm đếm lại");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-lg overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Đếm lại tiền mặt (Recount)</h3>
            <p className="text-xs text-muted-foreground">Ghi nhận số lần đếm mới vào biên bản đối soát</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <DenominationCalculator counts={counts} onChange={setCounts} />

        <div className="p-3.5 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-between">
          <span className="text-xs font-semibold text-primary">Tổng tiền kiểm đếm lần này:</span>
          <span className="font-mono text-lg font-bold text-primary">{formatVND(countedCash)}</span>
        </div>

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        <div className="flex items-center gap-3 mt-2">
          <Button type="button" variant="outline" onClick={onClose} className="w-1/2 h-12 rounded-xl">
            Huỷ
          </Button>
          <Button
            type="button"
            onClick={handleSubmit}
            disabled={isPending}
            className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
          >
            {isPending ? "Đang ghi..." : "Ghi nhận số đếm"}
          </Button>
        </div>
      </div>
    </div>
  );
}
