import { useState, type ReactElement } from "react";
import { UserRound } from "lucide-react";
import { useStaffCommands } from "../api/use-staff";
import {
  emptyForm,
  formFromRow,
  hasErrors,
  isFormChanged,
  isSelf,
  ROLE_LABELS,
  ROLES,
  toggleRole,
  validateForm,
  type FormErrors,
  type FormMode,
  type StaffForm,
  type StaffRow,
} from "../lib/staff";
import { useSessionStore } from "@/stores/use-session-store";
import { BTN_PRIMARY, BTN_SECONDARY, FormAlert, ModalShell } from "./toggle-enabled-dialog";

const INPUT =
  "h-12 min-h-[48px] w-full rounded-xl border border-slate-300 px-3 text-sm text-slate-800 outline-none transition focus:border-emerald-500 focus:ring-2 focus:ring-emerald-200 dark:border-border dark:bg-card dark:text-foreground";

function FieldError({ message }: { message?: string }): ReactElement | null {
  return message ? <p className="text-xs font-semibold text-rose-600">{message}</p> : null;
}

export function PinField({ label, value, error, onChange }: { label: string; value: string; error?: string; onChange: (v: string) => void }): ReactElement {
  return (
    <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-600 dark:text-muted-foreground">
      {label}
      <input
        type="password"
        inputMode="numeric"
        autoComplete="new-password"
        maxLength={8}
        value={value}
        onChange={(e) => onChange(e.target.value.replace(/\D/g, ""))}
        className={`${INPUT} font-mono tracking-widest`}
      />
      <FieldError message={error} />
    </label>
  );
}

export interface StaffFormPanelProps {
  mode: FormMode;
  form: StaffForm;
  errors: FormErrors;
  formError: string | null;
  managerLocked: boolean;
  busy: boolean;
  canSave: boolean;
  onChange: (form: StaffForm) => void;
  onSubmit: () => void;
  onClose: () => void;
}

export function StaffFormPanel({ mode, form, errors, formError, managerLocked, busy, canSave, onChange, onSubmit, onClose }: StaffFormPanelProps): ReactElement {
  return (
    <ModalShell labelledBy="staff-form-title">
      <div className="flex items-center gap-3">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/40">
          <UserRound className="h-5 w-5" />
        </div>
        <h2 id="staff-form-title" className="text-base font-bold text-slate-900 dark:text-foreground">
          {mode === "create" ? "Thêm nhân viên" : "Sửa nhân viên"}
        </h2>
      </div>

      <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-600 dark:text-muted-foreground">
        Tên hiển thị
        <input value={form.displayName} maxLength={120} onChange={(e) => onChange({ ...form, displayName: e.target.value })} className={INPUT} />
        <FieldError message={errors.displayName} />
      </label>

      <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-600 dark:text-muted-foreground">
        Mã đăng nhập
        <input
          value={form.loginCode}
          maxLength={24}
          onChange={(e) => onChange({ ...form, loginCode: e.target.value.toUpperCase() })}
          className={`${INPUT} font-mono uppercase tracking-wider`}
        />
        <FieldError message={errors.loginCode} />
      </label>

      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1.5 text-xs font-semibold text-slate-600 dark:text-muted-foreground">Vai trò</legend>
        {ROLES.map((role) => {
          const locked = role === "MANAGER" && managerLocked;
          return (
            <label key={role} className="flex min-h-[48px] items-center gap-3 rounded-xl border border-slate-200 px-3 text-sm font-semibold text-slate-700 dark:border-border dark:text-foreground">
              <input
                type="checkbox"
                aria-label={ROLE_LABELS[role]}
                checked={form.roles.includes(role)}
                disabled={locked}
                onChange={() => onChange({ ...form, roles: toggleRole(form.roles, role) })}
                className="h-5 w-5 accent-emerald-600"
              />
              {ROLE_LABELS[role]}
              {locked && <span className="text-xs font-normal text-slate-500">Không thể bỏ vai trò Quản lý của chính bạn</span>}
            </label>
          );
        })}
        <FieldError message={errors.roles} />
      </fieldset>

      {mode === "create" && (
        <>
          <PinField label="PIN" value={form.pin} error={errors.pin} onChange={(pin) => onChange({ ...form, pin })} />
          <PinField label="Xác nhận PIN" value={form.pinConfirm} error={errors.pinConfirm} onChange={(pinConfirm) => onChange({ ...form, pinConfirm })} />
          <label className="flex min-h-[48px] items-center gap-3 text-sm font-semibold text-slate-700 dark:text-foreground">
            <input type="checkbox" checked={form.enabled} onChange={(e) => onChange({ ...form, enabled: e.target.checked })} className="h-5 w-5 accent-emerald-600" />
            Kích hoạt ngay
          </label>
        </>
      )}

      <FormAlert message={formError} />

      <div className="flex justify-end gap-2">
        <button type="button" onClick={onClose} disabled={busy} className={BTN_SECONDARY}>
          Hủy bỏ
        </button>
        <button type="button" onClick={onSubmit} disabled={busy || !canSave} className={BTN_PRIMARY}>
          {busy ? "Đang lưu…" : "Lưu"}
        </button>
      </div>
    </ModalShell>
  );
}

/** `row === null` creates; otherwise edits that row. Mount with a `key` so each open starts fresh. */
export function StaffFormModal({ row, open, onClose }: { row: StaffRow | null; open: boolean; onClose: () => void }): ReactElement | null {
  const { create, update } = useStaffCommands();
  const selfId = useSessionStore((s) => s.staffId);
  const mode: FormMode = row ? "edit" : "create";
  const [form, setForm] = useState<StaffForm>(() => (row ? formFromRow(row) : emptyForm()));
  const [errors, setErrors] = useState<FormErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  if (!open) return null;

  const submit = async () => {
    const found = validateForm(form, mode);
    setErrors(found);
    setFormError(null);
    if (hasErrors(found)) return;
    setBusy(true);
    const result = row ? await update(row, form) : await create(form);
    setBusy(false);
    if (result.status === "done") onClose();
    else if (result.status === "failed") {
      if (result.error.code === "CONFLICT") setErrors({ loginCode: result.error.message });
      else setFormError(result.error.message);
    }
  };

  return (
    <StaffFormPanel
      mode={mode}
      form={form}
      errors={errors}
      formError={formError}
      managerLocked={isSelf(row, selfId)}
      busy={busy}
      canSave={!row || isFormChanged(form, row)}
      onChange={(next) => {
        setForm(next);
        setErrors({});
      }}
      onSubmit={() => void submit()}
      onClose={onClose}
    />
  );
}
