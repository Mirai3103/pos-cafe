// web/src/features/catalog/components/form-bits.tsx
import type { ReactElement, ReactNode } from "react";
import { Trash2, X, type LucideIcon } from "lucide-react";
import { useHotkeys } from "react-hotkeys-hook";
import { cn } from "@/lib/utils";

export const INPUT_CLASS =
  "h-12 min-h-[48px] w-full rounded-xl border border-slate-300 bg-white px-4 text-xs font-semibold text-slate-900 outline-none transition focus:border-emerald-500 focus:ring-2 focus:ring-emerald-200 sm:text-sm dark:border-border dark:bg-card dark:text-foreground";

export const MONEY_INPUT_CLASS = cn(INPUT_CLASS, "font-mono text-base font-bold text-emerald-700 dark:text-emerald-400");

/** An empty number input reads as 0 so validation reports it. */
export function readNumber(value: string): number {
  const n = Number(value);
  return Number.isNaN(n) ? 0 : n;
}

export interface ModalFrameProps {
  titleId: string;
  icon: LucideIcon;
  title: string;
  subtitle: string;
  wide?: boolean;
  /** Disables Esc and Ctrl+S while saving or while a child dialog is open. */
  busy: boolean;
  onClose: () => void;
  onSave: () => void;
  footer: ReactNode;
  children: ReactNode;
}

/** The prototype's modal chrome (settings.html #modal-item-form). */
export function ModalFrame({
  titleId,
  icon: Icon,
  title,
  subtitle,
  wide = false,
  busy,
  onClose,
  onSave,
  footer,
  children,
}: ModalFrameProps): ReactElement {
  useHotkeys("esc", onClose, { enabled: !busy, enableOnFormTags: true });
  useHotkeys(
    "mod+s",
    (event) => {
      event.preventDefault();
      onSave();
    },
    { enabled: !busy, enableOnFormTags: true },
  );
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-slate-900/40 p-3 backdrop-blur-xs sm:p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={cn(
          "flex max-h-[92vh] w-full flex-col overflow-hidden rounded-2xl border border-slate-200 bg-card shadow-modal animate-in fade-in zoom-in-95 dark:border-border",
          wide ? "max-w-4xl" : "max-w-xl",
        )}
      >
        <div className="flex items-start justify-between gap-3 border-b border-slate-100 p-4 sm:p-5 dark:border-border">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-emerald-200 bg-emerald-50 text-emerald-600 shadow-2xs dark:border-emerald-900 dark:bg-emerald-950/40">
              <Icon className="h-5 w-5" />
            </div>
            <div>
              <h3 id={titleId} className="text-base leading-tight font-bold text-slate-900 sm:text-lg dark:text-foreground">
                {title}
              </h3>
              <p className="mt-0.5 text-xs text-slate-500 dark:text-muted-foreground">{subtitle}</p>
            </div>
          </div>
          <button
            type="button"
            aria-label="Đóng"
            onClick={onClose}
            className="flex h-12 min-h-[48px] w-12 min-w-[48px] shrink-0 items-center justify-center rounded-xl text-slate-400 transition hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-muted"
          >
            <X className="h-5 w-5" />
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-4 sm:p-5">{children}</div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-100 bg-slate-50/60 p-4 dark:border-border dark:bg-muted/20">
          {footer}
        </div>
      </div>
    </div>
  );
}

export interface FieldProps {
  label: string;
  htmlFor?: string;
  required?: boolean;
  error?: string;
  hint?: string;
  children: ReactNode;
}

export function Field({ label, htmlFor, required, error, hint, children }: FieldProps): ReactElement {
  return (
    <div className="space-y-1.5">
      <label htmlFor={htmlFor} className="text-xs font-bold text-slate-700 dark:text-foreground">
        {label}
        {required && <span className="ml-0.5 text-rose-600">*</span>}
      </label>
      {children}
      {error ? (
        <p role="alert" className="text-[11px] font-semibold text-rose-600">
          {error}
        </p>
      ) : hint ? (
        <p className="text-[11px] text-slate-400">{hint}</p>
      ) : null}
    </div>
  );
}

export interface ChipProps {
  active: boolean;
  disabled?: boolean;
  title?: string;
  onClick: () => void;
  children: ReactNode;
}

export function Chip({ active, disabled, title, onClick, children }: ChipProps): ReactElement {
  return (
    <button
      type="button"
      aria-pressed={active}
      disabled={disabled}
      title={title}
      onClick={onClick}
      className={cn(
        "flex h-12 min-h-[48px] items-center gap-2 rounded-xl border px-3.5 text-xs font-semibold transition select-none active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50",
        active
          ? "border-emerald-600 bg-emerald-50 text-emerald-900 ring-2 ring-emerald-200 dark:bg-emerald-950/40 dark:text-emerald-300"
          : "border-slate-200 bg-white text-slate-700 hover:bg-slate-50 dark:border-border dark:bg-card dark:text-foreground dark:hover:bg-muted",
      )}
    >
      {children}
    </button>
  );
}

export interface FormFooterProps {
  retireLabel: string;
  onRetire?: () => void;
  onClose: () => void;
  onSave: () => void;
  saveLabel: string;
  busy: boolean;
}

export function FormFooter({ retireLabel, onRetire, onClose, onSave, saveLabel, busy }: FormFooterProps): ReactElement {
  return (
    <>
      <div>
        {onRetire && (
          <button
            type="button"
            onClick={onRetire}
            disabled={busy}
            className="flex h-12 min-h-[48px] items-center gap-2 rounded-xl border border-rose-200 bg-rose-50 px-4 text-xs font-bold text-rose-700 transition hover:bg-rose-100 active:scale-[0.98] sm:text-sm dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-400"
          >
            <Trash2 className="h-4 w-4" />
            {retireLabel}
          </button>
        )}
      </div>
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={onClose}
          disabled={busy}
          className="h-12 min-h-[48px] rounded-xl border border-slate-200 bg-white px-5 text-xs font-bold text-slate-700 transition hover:bg-slate-100 active:scale-[0.98] sm:text-sm dark:border-border dark:bg-card dark:text-foreground"
        >
          Hủy bỏ
        </button>
        <button
          type="button"
          onClick={onSave}
          disabled={busy}
          className="flex h-12 min-h-[48px] items-center gap-2 rounded-xl bg-emerald-600 px-6 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] disabled:opacity-60 sm:text-sm"
        >
          {busy ? "Đang lưu…" : saveLabel}
        </button>
      </div>
    </>
  );
}
