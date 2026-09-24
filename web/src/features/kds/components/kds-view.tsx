import { useEffect, useState, type ReactElement } from "react";
import { Coffee, WifiOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { messageForError } from "@/lib/error-messages";
import { usePreparationQueue } from "../api/use-preparation-queue";
import { usePreparationActions } from "../api/use-preparation-actions";
import {
  buildBoard,
  distinctCategories,
  summarizeBulkOutcomes,
  COLUMN_KEYS,
  type BoardUnit,
  type ColumnKey,
} from "../lib/board";
import { QueueColumn } from "./queue-column";
import { AlertsPanel } from "./alerts-panel";
import { CorrectionsLog } from "./corrections-log";
import { CategoryFilterBar } from "./category-filter-bar";
import { WasteDialog } from "./waste-dialog";
import { CorrectStateDialog } from "./correct-state-dialog";
import { KdsErrorToast } from "./kds-error-toast";

/** Reasons Remake accepts (internal/preparation/domain.go's remakeReasons) — narrower than Waste's. */
const REMAKE_REASONS = new Set(["PREPARATION_ERROR", "QUALITY_FAILURE", "OTHER"]);

// oxlint-disable-next-line react/only-export-components
export function remakeRequestFor(entry: { reason?: string; note?: string }): { reason: string; note?: string } {
  const reason = entry.reason && REMAKE_REASONS.has(entry.reason) ? entry.reason : "OTHER";
  if (reason !== "OTHER") return { reason, note: entry.note };

  const fallbackNote = entry.reason === "CUSTOMER_REQUEST" ? "Khách yêu cầu pha lại" : "Pha lại từ món đã huỷ";
  return { reason, note: entry.note?.trim() ? entry.note : fallbackNote };
}

export function KdsView(): ReactElement {
  const { data, isLoading, isError, refetch } = usePreparationQueue();
  const actions = usePreparationActions();

  const [categoryFilter, setCategoryFilter] = useState<string | null>(null);
  const [wasteTarget, setWasteTarget] = useState<BoardUnit | null>(null);
  const [correctTarget, setCorrectTarget] = useState<{ unit: BoardUnit; column: ColumnKey } | null>(null);
  const [failedUnitIds, setFailedUnitIds] = useState<ReadonlySet<string>>(new Set());
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [nowMs, setNowMs] = useState(() => Date.now());

  // Elapsed-time badges only need to refresh coarsely; the queue's own 5s
  // poll already redraws the board on every real change.
  useEffect(() => {
    const timer = setInterval(() => setNowMs(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);

  const handleAdvance = async (unitIds: string[], targetState: string) => {
    setFailedUnitIds(new Set());
    try {
      if (unitIds.length === 1) {
        await actions.advanceUnit(unitIds[0], targetState);
        return;
      }
      const outcomes = await actions.advanceMany(unitIds, targetState);
      const { failed } = summarizeBulkOutcomes(outcomes);
      setFailedUnitIds(
        new Set(
          outcomes.flatMap((outcome) =>
            outcome.status === "FAILED" && outcome.preparation_unit_id ? [outcome.preparation_unit_id] : [],
          ),
        ),
      );
      if (failed > 0) {
        setErrorMessage(`${failed} món không thể chuyển trạng thái do đã thay đổi. Danh sách đã được cập nhật.`);
      }
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  const handleAcknowledge = async (alertId: string) => {
    try {
      await actions.acknowledgeAlert(alertId);
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  // Carries the original Waste's own reason and note forward, rather than
  // asking the barista to re-describe an event that already happened.
  const handleRemake = async (entry: { id?: string; reason?: string; note?: string }) => {
    if (!entry.id) return;
    const { reason, note } = remakeRequestFor(entry);
    try {
      await actions.remakeWaste(entry.id, reason, note);
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full text-sm text-muted-foreground p-8">
        Đang tải hàng chờ pha chế...
      </div>
    );
  }

  // A failed 5s poll must not blank the always-on bar screen: react-query
  // keeps the last successful data alongside isError, so the full-screen
  // failure is reserved for a genuine first-load error.
  if (isError && !data) {
    return (
      <div className="flex items-center justify-center h-full text-sm text-destructive p-8">
        Không tải được hàng chờ pha chế. Vui lòng tải lại trang.
      </div>
    );
  }

  const units = data?.units ?? [];
  const categories = distinctCategories(units);
  const board = buildBoard(units, categoryFilter);

  return (
    <div className="flex flex-col gap-3 sm:gap-4 p-3 sm:p-4 md:p-6 h-full min-h-0 bg-slate-50 text-slate-900 overflow-hidden select-none">
      {/* Top Header Bar */}
      <div className="flex items-center justify-between gap-3 pb-2 border-b border-slate-200 shrink-0">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-emerald-50 text-emerald-700 flex items-center justify-center border border-emerald-200/60 font-bold shadow-2xs shrink-0">
            <Coffee className="w-5 h-5" />
          </div>
          <div className="flex flex-col text-left">
            <h1 className="text-sm sm:text-base font-bold text-slate-900 leading-tight">Màn hình Điều phối Pha chế (KDS)</h1>
            <span className="text-[11px] font-medium text-emerald-600 leading-tight flex items-center gap-1.5 mt-0.5">
              <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></span>
              Pha chế & Bếp · Tự động làm mới 5s
            </span>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <div className="hidden sm:flex items-center gap-2 px-3.5 h-10 rounded-xl bg-white border border-slate-200 text-xs text-slate-600 shadow-2xs">
            <span className="w-2 h-2 rounded-full bg-emerald-500"></span>
            <span>Đang chờ chế biến: <strong className="font-mono text-slate-900 font-bold">{units.length}</strong> món</span>
          </div>
        </div>
      </div>

      <AlertsPanel alerts={data?.alerts ?? []} busy={actions.isPending} onAcknowledge={handleAcknowledge} />

      <CategoryFilterBar categories={categories} active={categoryFilter} onSelect={setCategoryFilter} />

      {isError && (
        <div
          role="status"
          className="flex items-center justify-between gap-3 rounded-xl border border-destructive/40 bg-destructive/10 px-4 py-2 text-xs font-medium text-destructive"
        >
          <span className="flex items-center gap-2">
            <WifiOff className="h-4 w-4 shrink-0" />
            Mất kết nối tới máy chủ. Đang hiển thị dữ liệu mới nhất.
          </span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void refetch()}
            className="rounded-lg"
          >
            Thử lại
          </Button>
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-3 sm:gap-4 md:gap-6 flex-1 min-h-0">
        {COLUMN_KEYS.map((column) => (
          <QueueColumn
            key={column}
            column={column}
            tickets={board[column]}
            nowMs={nowMs}
            filterActive={categoryFilter !== null}
            busy={actions.isPending}
            failedUnitIds={failedUnitIds}
            onAdvance={handleAdvance}
            onRequestWaste={setWasteTarget}
            onRequestCorrectState={(unit) => setCorrectTarget({ unit, column })}
          />
        ))}
      </div>

      <CorrectionsLog corrections={data?.corrections ?? []} busy={actions.isPending} onRemake={handleRemake} />

      <WasteDialog unit={wasteTarget} onClose={() => setWasteTarget(null)} />
      <CorrectStateDialog
        unit={correctTarget?.unit ?? null}
        column={correctTarget?.column ?? null}
        onClose={() => setCorrectTarget(null)}
      />

      <KdsErrorToast
        message={errorMessage}
        onDismiss={() => {
          setErrorMessage(null);
          setFailedUnitIds(new Set());
        }}
      />
    </div>
  );
}
