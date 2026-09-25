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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4 backdrop-blur-xs">
      <div role="dialog" aria-modal="true" aria-labelledby="restore-title" className="flex w-full max-w-md flex-col gap-4 rounded-3xl border border-border bg-card p-6 shadow-2xl">
        <div className="flex items-center justify-between border-b border-border pb-3">
          <h2 id="restore-title" className="text-base font-bold text-foreground">Khôi phục tất cả còn hàng</h2>
          <button type="button" aria-label="Đóng" onClick={onClose} disabled={isPending} className="flex h-12 w-12 items-center justify-center rounded-xl text-muted-foreground hover:bg-muted">
            <X className="h-5 w-5" />
          </button>
        </div>
        <ul className="max-h-72 space-y-1 overflow-y-auto text-sm">
          {refs.map((ref) => (
            <li key={`${ref.kind}:${ref.id}`} className="rounded-lg bg-muted px-3 py-2 font-medium text-foreground">
              {ref.name}
            </li>
          ))}
        </ul>
        {error && <p role="alert" className="text-xs font-semibold text-destructive">{error}</p>}
        <div className="flex justify-end gap-2 border-t border-border pt-3">
          <button type="button" onClick={onClose} disabled={isPending} className="h-12 min-h-[48px] rounded-xl border border-border bg-card px-4 text-xs font-bold text-foreground hover:bg-muted">
            Hủy bỏ
          </button>
          <Button onClick={onConfirm} disabled={isPending || refs.length === 0} className="h-12 min-h-[48px] gap-2 rounded-xl px-6 font-bold">
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
}

export function RestoreAvailabilityDialog({ open, refs, onClose }: RestoreAvailabilityDialogProps): ReactElement | null {
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
