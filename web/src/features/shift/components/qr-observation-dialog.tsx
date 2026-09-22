import { useState, useEffect } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useRecordQRObservation } from "@/features/shift/api/use-shift";
import { newRequestId } from "@/lib/command";
import { playClick, playSuccess, playError } from "@/lib/sound";

interface QRObservationDialogProps {
  shiftId: string;
  isOpen: boolean;
  onClose: () => void;
  initialReceived?: number;
  initialRefunded?: number;
}

export function QRObservationDialog({
  shiftId,
  isOpen,
  onClose,
  initialReceived = 0,
  initialRefunded = 0,
}: QRObservationDialogProps) {
  const { recordQRObservation, isPending } = useRecordQRObservation(shiftId);
  const [received, setReceived] = useState(initialReceived.toString());
  const [refunded, setRefunded] = useState(initialRefunded.toString());
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (isOpen) {
      // oxlint-disable-next-line react/set-state-in-effect
      setReceived(initialReceived.toString());
      setRefunded(initialRefunded.toString());
      setError(null);
    }
  }, [isOpen, initialReceived, initialRefunded]);

  if (!isOpen) return null;

  const numReceived = parseInt(received.replace(/\D/g, ""), 10) || 0;
  const numRefunded = parseInt(refunded.replace(/\D/g, ""), 10) || 0;

  const handleSubmit = async () => {
    playClick();
    if (numReceived < 0 || numRefunded < 0) {
      playError();
      setError("Số tiền quan sát không được âm");
      return;
    }

    try {
      await recordQRObservation({
        request_id: newRequestId(),
        observed_received_vnd: numReceived,
        observed_refunded_vnd: numRefunded,
      });
      playSuccess();
      onClose();
    } catch (err: unknown) {
      playError();
      setError(err instanceof Error ? err.message : "Lỗi ghi nhận quan sát VietQR");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-md overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Đối soát VietQR qua App Ngân Hàng</h3>
            <p className="text-xs text-muted-foreground">Nhập số tiền thực tế ghi nhận từ biến động số dư tài khoản</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Tổng tiền VietQR đã nhận (VND)
          </label>
          <Input
            type="text"
            inputMode="numeric"
            value={received}
            onChange={(e) => setReceived(e.target.value)}
            className="font-mono text-lg font-bold h-12"
          />
        </div>

        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Tổng tiền VietQR đã hoàn trả (VND)
          </label>
          <Input
            type="text"
            inputMode="numeric"
            value={refunded}
            onChange={(e) => setRefunded(e.target.value)}
            className="font-mono text-lg font-bold h-12"
          />
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
            {isPending ? "Đang ghi..." : "Xác nhận đối soát QR"}
          </Button>
        </div>
      </div>
    </div>
  );
}
