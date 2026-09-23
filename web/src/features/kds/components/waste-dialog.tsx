import { useEffect, useRef, useState, type ReactElement } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { messageForError } from "@/lib/error-messages";
import { usePreparationActions } from "../api/use-preparation-actions";
import type { BoardUnit } from "../lib/board";

const WASTE_REASONS = [
  { value: "PREPARATION_ERROR", label: "Lỗi pha chế" },
  { value: "QUALITY_FAILURE", label: "Không đạt chất lượng" },
  { value: "CUSTOMER_REQUEST", label: "Khách yêu cầu" },
  { value: "OTHER", label: "Khác" },
] as const;

export interface WasteDialogProps {
  unit: BoardUnit | null;
  onClose: () => void;
}

export function WasteDialog({ unit, onClose }: WasteDialogProps): ReactElement | null {
  if (!unit) return null;
  return <WasteDialogForm key={unit.id} unit={unit} onClose={onClose} />;
}

function WasteDialogForm({ unit, onClose }: { unit: BoardUnit; onClose: () => void }) {
  const { wasteUnit, isPending } = usePreparationActions();
  const [reason, setReason] = useState<string>(WASTE_REASONS[0].value);
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const activeRef = useRef(true);
  const busy = isPending || isSubmitting;

  useEffect(() => {
    activeRef.current = true;
    return () => {
      activeRef.current = false;
    };
  }, []);

  function handleClose() {
    if (!isPending && !submittingRef.current) onClose();
  }

  async function handleSubmit() {
    if (isPending || submittingRef.current) return;
    if (reason === "OTHER" && !note.trim()) {
      setError("Vui lòng nhập ghi chú khi chọn lý do khác");
      return;
    }

    submittingRef.current = true;
    setIsSubmitting(true);
    try {
      await wasteUnit(unit.id, reason, note.trim() || undefined);
      if (!activeRef.current) return;
      setNote("");
      setReason(WASTE_REASONS[0].value);
      setError(null);
      onClose();
    } catch (err) {
      if (activeRef.current) setError(messageForError(err));
    } finally {
      submittingRef.current = false;
      if (activeRef.current) setIsSubmitting(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-xs animate-in fade-in duration-200">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="waste-dialog-title"
        className="flex w-full max-w-sm flex-col gap-4 overflow-hidden rounded-2xl border border-border bg-card p-6 shadow-xl"
      >
        <div className="flex items-start justify-between">
          <div>
            <h3 id="waste-dialog-title" className="text-base font-bold text-foreground">
              Huỷ món
            </h3>
            <p className="text-xs text-muted-foreground">
              {unit.itemName} #{unit.unitNumber}
            </p>
          </div>
          <button
            type="button"
            onClick={handleClose}
            disabled={busy}
            aria-label="Đóng"
            className="rounded-lg p-1 text-muted-foreground transition hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="space-y-1.5">
          <label htmlFor="waste-reason" className="text-xs font-semibold text-muted-foreground">
            Lý do
          </label>
          <select
            id="waste-reason"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            className="h-9 w-full rounded-lg border border-input bg-background px-2.5 text-xs font-medium"
          >
            {WASTE_REASONS.map((wasteReason) => (
              <option key={wasteReason.value} value={wasteReason.value}>
                {wasteReason.label}
              </option>
            ))}
          </select>
        </div>

        <div className="space-y-1.5">
          <label htmlFor="waste-note" className="text-xs font-semibold text-muted-foreground">
            Ghi chú {reason === "OTHER" ? "(bắt buộc)" : "(không bắt buộc)"}
          </label>
          <Input
            id="waste-note"
            value={note}
            onChange={(event) => setNote(event.target.value)}
            placeholder="Mô tả lý do..."
          />
        </div>

        {error && <p className="text-center text-xs font-medium text-destructive">{error}</p>}

        <div className="mt-2 flex items-center gap-3">
          <Button
            type="button"
            variant="outline"
            onClick={handleClose}
            disabled={busy}
            className="h-12 w-1/2 rounded-xl"
          >
            Huỷ bỏ
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={handleSubmit}
            disabled={busy}
            className="h-12 w-1/2 rounded-xl font-bold"
          >
            {busy ? "Đang xử lý..." : "Xác nhận huỷ món"}
          </Button>
        </div>
      </div>
    </div>
  );
}
