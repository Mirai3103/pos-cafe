import { useState, type ReactElement } from "react";
import { KeyRound } from "lucide-react";
import { useStaffCommands } from "../api/use-staff";
import { hasErrors, validatePin, type FormErrors, type StaffRow } from "../lib/staff";
import { BTN_PRIMARY, BTN_SECONDARY, FormAlert, ModalShell, SIGN_OUT_WARNING } from "./toggle-enabled-dialog";
import { PinField } from "./staff-form-modal";

export interface ResetPinPanelProps {
  name: string;
  pin: string;
  pinConfirm: string;
  errors: FormErrors;
  busy: boolean;
  error: string | null;
  onChange: (pin: string, pinConfirm: string) => void;
  onConfirm: () => void;
  onClose: () => void;
}

export function ResetPinPanel({ name, pin, pinConfirm, errors, busy, error, onChange, onConfirm, onClose }: ResetPinPanelProps): ReactElement {
  return (
    <ModalShell labelledBy="reset-pin-title">
      <div className="flex items-center gap-3">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900 dark:bg-amber-950/40">
          <KeyRound className="h-5 w-5" />
        </div>
        <div>
          <h2 id="reset-pin-title" className="text-base font-bold text-slate-900 dark:text-foreground">
            Đặt lại PIN cho “{name}”
          </h2>
          <p className="text-xs text-slate-500 dark:text-muted-foreground">{SIGN_OUT_WARNING}</p>
        </div>
      </div>
      <PinField label="PIN mới" value={pin} error={errors.pin} onChange={(v) => onChange(v, pinConfirm)} />
      <PinField label="Xác nhận PIN" value={pinConfirm} error={errors.pinConfirm} onChange={(v) => onChange(pin, v)} />
      <FormAlert message={error} />
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onClose} disabled={busy} className={BTN_SECONDARY}>
          Hủy bỏ
        </button>
        <button type="button" onClick={onConfirm} disabled={busy} className={BTN_PRIMARY}>
          {busy ? "Đang xử lý…" : "Đặt lại PIN"}
        </button>
      </div>
    </ModalShell>
  );
}

export function ResetPinModal({ row, onClose }: { row: StaffRow | null; onClose: () => void }): ReactElement | null {
  const { resetPin } = useStaffCommands();
  const [pin, setPin] = useState("");
  const [pinConfirm, setPinConfirm] = useState("");
  const [errors, setErrors] = useState<FormErrors>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  if (!row) return null;

  const close = () => {
    setPin("");
    setPinConfirm("");
    setErrors({});
    setError(null);
    onClose();
  };
  const confirm = async () => {
    const found = validatePin(pin, pinConfirm);
    setErrors(found);
    if (hasErrors(found)) return;
    setBusy(true);
    const result = await resetPin(row, pin);
    setBusy(false);
    if (result.status === "done") close();
    else if (result.status === "failed") setError(result.error.message);
  };

  return (
    <ResetPinPanel
      name={row.displayName}
      pin={pin}
      pinConfirm={pinConfirm}
      errors={errors}
      busy={busy}
      error={error}
      onChange={(p, c) => {
        setPin(p);
        setPinConfirm(c);
        setErrors({});
      }}
      onConfirm={() => void confirm()}
      onClose={close}
    />
  );
}
