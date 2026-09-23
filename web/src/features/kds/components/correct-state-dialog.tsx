import { useEffect, useRef, useState, type ReactElement } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { messageForError } from "@/lib/error-messages";
import { useManagerApprovalStore } from "@/stores/use-manager-approval-store";
import { usePreparationActions } from "../api/use-preparation-actions";
import { CORRECT_TARGET, type BoardUnit, type ColumnKey } from "../lib/board";

const CORRECTION_REASONS = [
  { value: "STATE_RECORDED_IN_ERROR", label: "Ghi nhận nhầm thao tác" },
  { value: "OTHER", label: "Khác" },
] as const;

export interface CorrectStateDialogProps {
  unit: BoardUnit | null;
  column: ColumnKey | null;
  onClose: () => void;
}

export function CorrectStateDialog({
  unit,
  column,
  onClose,
}: CorrectStateDialogProps): ReactElement | null {
  const targetState = column ? CORRECT_TARGET[column] : null;
  if (!unit || !column || !targetState) return null;

  return (
    <CorrectStateDialogForm
      key={`${unit.id}:${column}`}
      unit={unit}
      targetState={targetState}
      onClose={onClose}
    />
  );
}

function CorrectStateDialogForm({
  unit,
  targetState,
  onClose,
}: {
  unit: BoardUnit;
  targetState: string;
  onClose: () => void;
}) {
  const { correctState, isPending } = usePreparationActions();
  const promptApproval = useManagerApprovalStore((state) => state.promptApproval);
  const [reason, setReason] = useState<string>(CORRECTION_REASONS[0].value);
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
      const { managerPin } = await promptApproval({
        title: "Duyệt hoàn tác thao tác",
        description: `Hoàn tác "${unit.itemName}" #${unit.unitNumber} về trạng thái trước đó.`,
        confirmLabel: "Xác nhận hoàn tác",
      });
      if (!activeRef.current) return;

      await correctState(unit.id, targetState, reason, managerPin, note.trim() || undefined);
      if (!activeRef.current) return;

      setNote("");
      setReason(CORRECTION_REASONS[0].value);
      setError(null);
      onClose();
    } catch (err) {
      if (!activeRef.current || isApprovalDismissal(err)) return;
      setError(messageForError(err));
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
        aria-labelledby="correct-state-dialog-title"
        className="flex w-full max-w-sm flex-col gap-4 overflow-hidden rounded-2xl border border-border bg-card p-6 shadow-xl"
      >
        <div className="flex items-start justify-between">
          <div>
            <h3 id="correct-state-dialog-title" className="text-base font-bold text-foreground">
              Hoàn tác thao tác
            </h3>
            <p className="text-xs text-muted-foreground">
              {unit.itemName} #{unit.unitNumber} — cần Quản lý duyệt
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
          <label htmlFor="correction-reason" className="text-xs font-semibold text-muted-foreground">
            Lý do
          </label>
          <select
            id="correction-reason"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            disabled={busy}
            className="h-9 w-full rounded-lg border border-input bg-background px-2.5 text-xs font-medium disabled:opacity-50"
          >
            {CORRECTION_REASONS.map((correctionReason) => (
              <option key={correctionReason.value} value={correctionReason.value}>
                {correctionReason.label}
              </option>
            ))}
          </select>
        </div>

        <div className="space-y-1.5">
          <label htmlFor="correction-note" className="text-xs font-semibold text-muted-foreground">
            Ghi chú {reason === "OTHER" ? "(bắt buộc)" : "(không bắt buộc)"}
          </label>
          <Input
            id="correction-note"
            value={note}
            onChange={(event) => setNote(event.target.value)}
            placeholder="Mô tả lý do..."
            disabled={busy}
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
            onClick={handleSubmit}
            disabled={busy}
            className="h-12 w-1/2 rounded-xl font-bold"
          >
            {busy ? "Đang xử lý..." : "Yêu cầu Quản lý duyệt"}
          </Button>
        </div>
      </div>
    </div>
  );
}

function isApprovalDismissal(error: unknown): boolean {
  return (
    error instanceof Error &&
    (error.message === "MANAGER_APPROVAL_CANCELLED" ||
      error.message === "MANAGER_APPROVAL_SUPERSEDED")
  );
}
