// web/src/features/catalog/components/save-progress.tsx
import type { ReactElement } from "react";
import { Check, Circle, X } from "lucide-react";
import type { StepResult } from "../lib/run-plan";

export const SAVE_INCOMPLETE = "Lưu chưa hoàn tất. Sửa lỗi rồi bấm Lưu để tiếp tục phần còn lại.";

/** Shown after a save stopped part-way (spec §4.4). */
export function SaveProgress({ results }: { results: StepResult[] | null }): ReactElement | null {
  if (!results || results.every((r) => r.status === "done")) return null;
  return (
    <div role="alert" className="space-y-2 rounded-xl border border-rose-200 bg-rose-50/60 p-3 dark:border-rose-900 dark:bg-rose-950/20">
      <p className="text-xs font-bold text-rose-700 dark:text-rose-400">{SAVE_INCOMPLETE}</p>
      <ul className="space-y-1 text-xs">
        {results.map((r, i) => (
          <li key={i} className="flex items-start gap-2">
            {r.status === "done" ? (
              <Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-600" />
            ) : r.status === "failed" ? (
              <X className="mt-0.5 h-3.5 w-3.5 shrink-0 text-rose-600" />
            ) : (
              <Circle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-slate-300" />
            )}
            <span className={r.status === "pending" ? "text-slate-400" : "text-slate-800 dark:text-foreground"}>
              {r.label}
              {r.error && <span className="font-semibold text-rose-700 dark:text-rose-400"> — {r.error}</span>}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
