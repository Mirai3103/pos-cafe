import { useEffect, type ReactElement } from "react";
import { AlertCircle, CheckCircle } from "lucide-react";
import { cn } from "@/lib/utils";

export interface StockToastMessage {
  text: string;
  tone: "success" | "error";
}

/** How long a toast stays up, as in the design prototype. */
export const STOCK_TOAST_MS = 2_800;

/** The design's dark bottom-right toast for availability changes. */
export function StockToast({ toast, onDismiss }: { toast: StockToastMessage | null; onDismiss: () => void }): ReactElement | null {
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(onDismiss, STOCK_TOAST_MS);
    return () => clearTimeout(timer);
  }, [toast, onDismiss]);

  if (!toast) return null;
  const isError = toast.tone === "error";
  return (
    <div role={isError ? "alert" : "status"} className="pointer-events-none fixed right-6 bottom-6 z-50 animate-in fade-in slide-in-from-bottom-4">
      <div className="flex min-h-[48px] items-center gap-3 rounded-xl border border-slate-700/80 bg-slate-900 px-4 py-3 text-xs font-medium text-white shadow-xl sm:text-sm">
        <div
          className={cn(
            "flex h-6 w-6 shrink-0 items-center justify-center rounded-lg",
            isError ? "bg-rose-500/20 text-rose-400" : "bg-emerald-500/20 text-emerald-400",
          )}
        >
          {isError ? <AlertCircle className="h-4 w-4" /> : <CheckCircle className="h-4 w-4" />}
        </div>
        <span className="text-slate-100">{toast.text}</span>
      </div>
    </div>
  );
}
