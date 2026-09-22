import { useState } from "react";
import { X, Play, Calculator } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { DenominationCalculator } from "./denomination-calculator";
import {
  type DenominationCounts,
  calculateDenominationTotal,
} from "@/features/shift/utils/denomination";
import { useOpenShift } from "@/features/shift/api/use-shift";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";

interface OpenShiftDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

export function OpenShiftDialog({ isOpen, onClose }: OpenShiftDialogProps) {
  const { openShift, isPending } = useOpenShift();
  const [showCalculator, setShowCalculator] = useState(false);
  const [directAmount, setDirectAmount] = useState<string>("1000000");
  const [counts, setCounts] = useState<DenominationCounts>({
    500_000: 1,
    200_000: 2,
    100_000: 1,
  });
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const currentTotal = showCalculator
    ? calculateDenominationTotal(counts)
    : parseInt(directAmount.replace(/\D/g, ""), 10) || 0;

  const handleOpen = async () => {
    playClick();
    if (currentTotal < 0) {
      playError();
      setError("Số tiền đầu ca không được âm");
      return;
    }

    try {
      await openShift({
        request_id: newRequestId(),
        opening_float_vnd: currentTotal,
      });
      playSuccess();
      onClose();
    } catch (err: unknown) {
      playError();
      setError(err instanceof Error ? err.message : "Không thể mở ca làm việc");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-lg overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Mở ca làm việc (Opening Shift)</h3>
            <p className="text-xs text-muted-foreground">Thiết lập số tiền mặt có sẵn trong két đầu ca</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Mode Toggle */}
        <div className="flex items-center justify-between">
          <span className="text-xs font-semibold text-muted-foreground">Cách nhập số tiền:</span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setShowCalculator(!showCalculator)}
            className="h-8 text-xs font-medium"
          >
            <Calculator className="w-3.5 h-3.5 mr-1" />
            {showCalculator ? "Nhập số tiền trực tiếp" : "Bảng kê mệnh giá"}
          </Button>
        </div>

        {/* Input area */}
        {showCalculator ? (
          <DenominationCalculator counts={counts} onChange={setCounts} />
        ) : (
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-muted-foreground">Số tiền đầu ca (VND)</label>
            <Input
              type="text"
              inputMode="numeric"
              value={directAmount}
              onChange={(e) => setDirectAmount(e.target.value)}
              className="font-mono text-xl font-bold h-12"
              placeholder="1000000"
            />
          </div>
        )}

        {/* Total Summary */}
        <div className="p-3.5 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-between">
          <span className="text-xs font-semibold text-primary">Tổng tiền mặt bàn giao:</span>
          <span className="font-mono text-lg font-bold text-primary">{formatVND(currentTotal)}</span>
        </div>

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        {/* Action buttons */}
        <div className="flex items-center gap-3 mt-2">
          <Button type="button" variant="outline" onClick={onClose} className="w-1/2 h-12 rounded-xl">
            Huỷ
          </Button>
          <Button
            type="button"
            onClick={handleOpen}
            disabled={isPending}
            className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
          >
            <Play className="w-4 h-4 fill-current mr-1.5" />
            {isPending ? "Đang mở..." : "Xác nhận mở ca"}
          </Button>
        </div>
      </div>
    </div>
  );
}
