import { useState, type ReactElement } from "react";
import { Lock, Unlock } from "lucide-react";
import { useStaffCommands } from "../api/use-staff";
import type { StaffRow } from "../lib/staff";

export const SIGN_OUT_WARNING = "Nhân viên sẽ bị đăng xuất trên mọi thiết bị.";

export const BTN_SECONDARY =
  "h-12 min-h-[48px] rounded-xl border border-slate-200 bg-white px-5 text-xs font-bold text-slate-700 hover:bg-slate-100 disabled:opacity-60 sm:text-sm dark:border-border dark:bg-card dark:text-foreground";
export const BTN_PRIMARY =
  "h-12 min-h-[48px] rounded-xl bg-slate-900 px-6 text-xs font-bold text-white hover:bg-slate-800 disabled:opacity-60 sm:text-sm dark:bg-foreground dark:text-background";
export const BTN_DANGER =
  "h-12 min-h-[48px] rounded-xl bg-rose-600 px-6 text-xs font-bold text-white hover:bg-rose-700 disabled:opacity-60 sm:text-sm";

export function ModalShell({ labelledBy, children }: { labelledBy: string; children: React.ReactNode }): ReactElement {
  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-xs">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        className="flex max-h-[90vh] w-full max-w-md flex-col gap-4 overflow-y-auto rounded-2xl border border-slate-200 bg-card p-5 shadow-modal animate-in fade-in zoom-in-95 dark:border-border"
      >
        {children}
      </div>
    </div>
  );
}

export function FormAlert({ message }: { message: string | null }): ReactElement | null {
  if (!message) return null;
  return (
    <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-xs font-semibold text-rose-700 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-400">
      {message}
    </p>
  );
}

export interface ToggleEnabledPanelProps {
  name: string;
  enabling: boolean;
  busy: boolean;
  error: string | null;
  onConfirm: () => void;
  onClose: () => void;
}

export function ToggleEnabledPanel({ name, enabling, busy, error, onConfirm, onClose }: ToggleEnabledPanelProps): ReactElement {
  const Icon = enabling ? Unlock : Lock;
  return (
    <ModalShell labelledBy="toggle-title">
      <div className="flex items-center gap-3">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-slate-700 dark:border-border dark:bg-muted">
          <Icon className="h-5 w-5" />
        </div>
        <div>
          <h2 id="toggle-title" className="text-base font-bold text-slate-900 dark:text-foreground">
            {enabling ? `Mở khóa “${name}”` : `Khóa “${name}”`}
          </h2>
          <p className="text-xs text-slate-500 dark:text-muted-foreground">
            {enabling ? "Nhân viên có thể đăng nhập lại bằng PIN hiện tại." : SIGN_OUT_WARNING}
          </p>
        </div>
      </div>
      <FormAlert message={error} />
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onClose} disabled={busy} className={BTN_SECONDARY}>
          Hủy bỏ
        </button>
        <button type="button" onClick={onConfirm} disabled={busy} className={enabling ? BTN_PRIMARY : BTN_DANGER}>
          {busy ? "Đang xử lý…" : enabling ? "Mở khóa" : "Khóa tài khoản"}
        </button>
      </div>
    </ModalShell>
  );
}

export function ToggleEnabledDialog({ row, onClose }: { row: StaffRow | null; onClose: () => void }): ReactElement | null {
  const { setEnabled } = useStaffCommands();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (!row) return null;

  const close = () => {
    setError(null);
    onClose();
  };
  const confirm = async () => {
    setBusy(true);
    const result = await setEnabled(row, !row.enabled);
    setBusy(false);
    if (result.status === "done") close();
    else if (result.status === "failed") setError(result.error.message);
  };

  return <ToggleEnabledPanel name={row.displayName} enabling={!row.enabled} busy={busy} error={error} onConfirm={() => void confirm()} onClose={close} />;
}
