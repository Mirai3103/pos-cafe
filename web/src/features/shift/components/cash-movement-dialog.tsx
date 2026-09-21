import { useState } from "react";
import { X, ArrowDownRight, ArrowUpRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useRecordCashMovement } from "@/features/shift/api/use-shift";
import { useManagerApprovalStore } from "@/stores/use-manager-approval-store";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";

export type ShiftRecordCashMovementCommandReason =
  | "ADD_CHANGE_FUND"
  | "REMOVE_EXCESS_FLOAT"
  | "SAFE_DROP"
  | "OTHER";

interface CashMovementDialogProps {
  shiftId: string;
  isOpen: boolean;
  onClose: () => void;
  defaultMethod?: "PAY_IN" | "PAY_OUT";
}

const REASONS: { value: ShiftRecordCashMovementCommandReason; label: string }[] = [
  { value: "ADD_CHANGE_FUND", label: "Bổ sung tiền thối lẻ (Change Fund)" },
  { value: "REMOVE_EXCESS_FLOAT", label: "Rút bớt tiền mặt dư thừa" },
  { value: "SAFE_DROP", label: "Chuyển tiền vào két an toàn (Safe Drop)" },
  { value: "OTHER", label: "Lý do khác (Bắt buộc ghi chú)" },
];

export function CashMovementDialog({
  shiftId,
  isOpen,
  onClose,
  defaultMethod = "PAY_IN",
}: CashMovementDialogProps) {
  const { recordCashMovement, isPending } = useRecordCashMovement(shiftId);
  const promptApproval = useManagerApprovalStore((s) => s.promptApproval);

  const [method, setMethod] = useState<"PAY_IN" | "PAY_OUT">(defaultMethod);
  const [amount, setAmount] = useState<string>("100000");
  const [reason, setReason] = useState<ShiftRecordCashMovementCommandReason>("ADD_CHANGE_FUND");
  const [note, setNote] = useState<string>("");
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const numAmount = parseInt(amount.replace(/\D/g, ""), 10) || 0;

  const handleSubmit = async () => {
    playClick();
    if (numAmount <= 0) {
      playError();
      setError("Số tiền phải lớn hơn 0");
      return;
    }
    if (reason === "OTHER" && !note.trim()) {
      playError();
      setError("Lý do khác bắt buộc phải nhập ghi chú");
      return;
    }

    try {
      // Prompt Manager Approval
      const creds = await promptApproval({
        title: method === "PAY_IN" ? "Duyệt Nộp Quỹ Tiền Mặt" : "Duyệt Rút Quỹ Tiền Mặt",
        description: `Xác nhận ${method === "PAY_IN" ? "nộp thêm" : "rút chi"} ${formatVND(numAmount)} tiền mặt.`,
        confirmLabel: "Phê duyệt biến động quỹ",
      });

      await recordCashMovement({
        request_id: newRequestId(),
        method,
        amount_vnd: numAmount,
        reason,
        note: note.trim() ? note.trim() : undefined,
        approver_login_code: creds.approverLoginCode,
        manager_pin: creds.managerPin,
      });

      playSuccess();
      onClose();
    } catch (err: unknown) {
      playError();
      if (err instanceof Error && err.message === "MANAGER_APPROVAL_CANCELLED") {
        return;
      }
      setError(err instanceof Error ? err.message : "Lỗi ghi nhận biến động tiền mặt");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-md overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Giao dịch tiền mặt (Pay In / Out)</h3>
            <p className="text-xs text-muted-foreground">Yêu cầu Quản lý phê duyệt thao tác</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Method Toggle */}
        <div className="grid grid-cols-2 gap-2 p-1 rounded-xl bg-muted">
          <button
            type="button"
            onClick={() => setMethod("PAY_IN")}
            className={`min-h-10 h-10 rounded-lg text-xs font-bold flex items-center justify-center gap-1.5 transition ${
              method === "PAY_IN" ? "bg-background text-emerald-600 shadow-xs" : "text-muted-foreground"
            }`}
          >
            <ArrowDownRight className="w-4 h-4" />
            Nộp tiền (Pay In)
          </button>
          <button
            type="button"
            onClick={() => setMethod("PAY_OUT")}
            className={`min-h-10 h-10 rounded-lg text-xs font-bold flex items-center justify-center gap-1.5 transition ${
              method === "PAY_OUT" ? "bg-background text-rose-600 shadow-xs" : "text-muted-foreground"
            }`}
          >
            <ArrowUpRight className="w-4 h-4" />
            Rút tiền (Pay Out)
          </button>
        </div>

        {/* Amount */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Số tiền (VND)</label>
          <Input
            type="text"
            inputMode="numeric"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            className="font-mono text-xl font-bold h-12"
          />
          <div className="flex gap-1.5">
            {[50_000, 100_000, 200_000, 500_000].map((quick) => (
              <button
                key={quick}
                type="button"
                onClick={() => setAmount(quick.toString())}
                className="px-2 py-1 rounded-md text-[11px] font-mono font-medium border border-border bg-muted/40 hover:bg-muted transition"
              >
                +{quick / 1000}k
              </button>
            ))}
          </div>
        </div>

        {/* Reason Select */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Lý do giao dịch</label>
          <select
            value={reason}
            onChange={(e) => setReason(e.target.value as ShiftRecordCashMovementCommandReason)}
            className="w-full h-11 px-3 rounded-xl border border-input bg-background text-sm font-medium text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
          >
            {REASONS.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </select>
        </div>

        {/* Note */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Ghi chú {reason === "OTHER" ? "(Bắt buộc)" : "(Tuỳ chọn)"}
          </label>
          <Input
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="Nhập lý do chi tiết..."
          />
        </div>

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        {/* Submit */}
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
            {isPending ? "Đang ghi..." : "Yêu cầu Quản lý duyệt"}
          </Button>
        </div>
      </div>
    </div>
  );
}
