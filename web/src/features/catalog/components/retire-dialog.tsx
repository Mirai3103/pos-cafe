// web/src/features/catalog/components/retire-dialog.tsx
import { useState, type ReactElement } from "react";
import { Trash2 } from "lucide-react";
import { RETIRE_REASON_LABELS, RETIRE_REASONS, type Retirement } from "../lib/catalog-model";
import { Chip } from "./form-bits";

export const RETIRE_WARNING = "Không thể hoàn tác. Lịch sử bán hàng vẫn được giữ.";
export const NOTE_REQUIRED = "Vui lòng ghi chú lý do";

const INITIAL: Retirement = { reason: "NO_LONGER_OFFERED", note: "" };

export interface RetirePanelProps {
  name: string;
  value: Retirement;
  error: string | null;
  busy: boolean;
  onChange: (value: Retirement) => void;
  onConfirm: () => void;
  onClose: () => void;
}

export function RetirePanel({ name, value, error, busy, onChange, onConfirm, onClose }: RetirePanelProps): ReactElement {
  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-xs">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="retire-title"
        className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-slate-200 bg-card p-5 shadow-modal animate-in fade-in zoom-in-95 dark:border-border"
      >
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-rose-200 bg-rose-50 text-rose-600 dark:border-rose-900 dark:bg-rose-950/40">
            <Trash2 className="h-5 w-5" />
          </div>
          <div>
            <h2 id="retire-title" className="text-base font-bold text-slate-900 dark:text-foreground">
              Ngừng bán “{name}”
            </h2>
            <p className="text-xs text-slate-500 dark:text-muted-foreground">{RETIRE_WARNING}</p>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          {RETIRE_REASONS.map((reason) => (
            <Chip key={reason} active={value.reason === reason} onClick={() => onChange({ ...value, reason })}>
              {RETIRE_REASON_LABELS[reason]}
            </Chip>
          ))}
        </div>
        <textarea
          aria-label="Ghi chú"
          rows={2}
          value={value.note}
          placeholder="Ghi chú (bắt buộc khi chọn Khác)"
          onChange={(e) => onChange({ ...value, note: e.target.value })}
          className="min-h-[72px] w-full resize-none rounded-xl border border-slate-300 p-3 text-xs text-slate-700 outline-none transition focus:border-emerald-500 focus:ring-2 focus:ring-emerald-200 dark:border-border dark:bg-card dark:text-foreground"
        />
        {error && (
          <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-xs font-semibold text-rose-700 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-400">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="h-12 min-h-[48px] rounded-xl border border-slate-200 bg-white px-5 text-xs font-bold text-slate-700 hover:bg-slate-100 sm:text-sm dark:border-border dark:bg-card dark:text-foreground"
          >
            Hủy bỏ
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={busy}
            className="h-12 min-h-[48px] rounded-xl bg-rose-600 px-6 text-xs font-bold text-white hover:bg-rose-700 disabled:opacity-60 sm:text-sm"
          >
            {busy ? "Đang xử lý…" : "Ngừng bán"}
          </button>
        </div>
      </div>
    </div>
  );
}

export interface RetireDialogProps {
  open: boolean;
  name: string;
  /** Resolves to an error message to show, or null when done. */
  onConfirm: (retirement: Retirement) => Promise<string | null> | string | null;
  onClose: () => void;
}

export function RetireDialog({ open, name, onConfirm, onClose }: RetireDialogProps): ReactElement | null {
  const [value, setValue] = useState<Retirement>(INITIAL);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  if (!open) return null;

  const reset = () => {
    setValue(INITIAL);
    setError(null);
  };
  const confirm = async () => {
    if (value.reason === "OTHER" && !value.note.trim()) {
      setError(NOTE_REQUIRED);
      return;
    }
    setBusy(true);
    const failure = await onConfirm(value);
    setBusy(false);
    if (failure) setError(failure);
    else reset();
  };

  return (
    <RetirePanel
      name={name}
      value={value}
      error={error}
      busy={busy}
      onChange={(v) => {
        setValue(v);
        setError(null);
      }}
      onConfirm={() => void confirm()}
      onClose={() => {
        reset();
        onClose();
      }}
    />
  );
}
