import { useState, type ReactElement } from "react";
import { RefreshCw, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { messageForError } from "@/lib/error-messages";
import { playErrorBuzz, playSuccessChirp } from "@/lib/sound";
import { STALE_RESTORE_MESSAGE, classifyRestoreFailure, useRestoreAvailability } from "../api/use-availability";
import { nextIntent, refsKey, type AvailabilityRef, type Intent } from "../lib/availability";

export interface RestorePanelProps {
  refs: AvailabilityRef[];
  error: string | null;
  isPending: boolean;
  onConfirm: () => void;
  onClose: () => void;
}

export function RestorePanel({ refs, error, isPending, onConfirm, onClose }: RestorePanelProps): ReactElement {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-xs">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="restore-title"
        className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-slate-200 bg-card p-5 shadow-modal animate-in fade-in zoom-in-95 dark:border-border"
      >
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-emerald-200 bg-emerald-50 text-emerald-600 dark:border-emerald-900 dark:bg-emerald-950/40">
              <RefreshCw className="h-5 w-5" />
            </div>
            <div>
              <h2 id="restore-title" className="text-base font-bold text-slate-900 dark:text-foreground">Khôi phục tất cả Còn hàng</h2>
              <p className="text-xs text-slate-500 dark:text-muted-foreground">Các mục sau sẽ được mở bán lại trên POS</p>
            </div>
          </div>
          <button
            type="button"
            aria-label="Đóng"
            onClick={onClose}
            disabled={isPending}
            className="flex h-12 min-h-[48px] w-12 min-w-[48px] items-center justify-center rounded-xl text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-muted"
          >
            <X className="h-5 w-5" />
          </button>
        </div>
        <ul className="max-h-72 space-y-1.5 overflow-y-auto text-sm">
          {refs.map((ref) => (
            <li
              key={`${ref.kind}:${ref.id}`}
              className="flex items-center gap-2 rounded-xl border border-rose-200 bg-rose-50/60 px-3 py-2 font-medium text-slate-800 dark:border-rose-900 dark:bg-rose-950/20 dark:text-foreground"
            >
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-rose-600" />
              <span>{ref.name}</span>
            </li>
          ))}
        </ul>
        {error && (
          <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-xs font-semibold text-rose-700 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-400">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2 border-t border-slate-100 pt-4 dark:border-border">
          <button
            type="button"
            onClick={onClose}
            disabled={isPending}
            className="h-12 min-h-[48px] rounded-xl border border-slate-200 bg-card px-4 text-xs font-bold text-slate-700 hover:bg-slate-50 sm:text-sm dark:border-border dark:text-foreground dark:hover:bg-muted"
          >
            Hủy bỏ
          </button>
          <Button
            onClick={onConfirm}
            disabled={isPending || refs.length === 0}
            className="h-12 min-h-[48px] gap-2 rounded-xl bg-emerald-600 px-6 text-xs font-bold text-white hover:bg-emerald-700 sm:text-sm"
          >
            <RefreshCw className="h-4 w-4" />
            {isPending ? "Đang khôi phục..." : `Khôi phục ${refs.length} mục`}
          </Button>
        </div>
      </div>
    </div>
  );
}

export interface RestoreAvailabilityDialogProps {
  open: boolean;
  refs: AvailabilityRef[];
  onClose: () => void;
  /** Called after the batch succeeded, before the dialog closes. */
  onRestored?: () => void;
}

export function RestoreAvailabilityDialog({ open, refs, onClose, onRestored }: RestoreAvailabilityDialogProps): ReactElement | null {
  const { restore, refresh, isPending } = useRestoreAvailability();
  const [error, setError] = useState<string | null>(null);
  const [intent, setIntent] = useState<Intent | null>(null);
  // Tracked in state (not a ref) so the reset below runs as a render-time
  // state adjustment rather than a post-commit effect.
  const [wasOpen, setWasOpen] = useState(open);

  if (open !== wasOpen) {
    setWasOpen(open);
    if (!open) {
      setIntent(null);
      setError(null);
    }
  }

  if (!open) return null;

  // Same list, same request_id across retries; a changed list is a new intent.
  const key = refsKey(refs);
  if (!intent || intent.key !== key) {
    setIntent(nextIntent(intent, key));
  }

  async function handleConfirm() {
    if (isPending || !intent) return;
    setError(null);
    try {
      await restore(refs, intent.id);
      playSuccessChirp();
      onRestored?.();
      onClose();
    } catch (err) {
      playErrorBuzz();
      if (classifyRestoreFailure(err) === "stale") {
        setError(STALE_RESTORE_MESSAGE);
        await refresh();
      } else {
        setError(messageForError(err));
      }
    }
  }

  return <RestorePanel refs={refs} error={error} isPending={isPending} onConfirm={handleConfirm} onClose={onClose} />;
}
