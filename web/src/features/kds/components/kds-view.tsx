import { useEffect, useState, type ReactElement } from "react";
import { ChefHat } from "lucide-react";
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
  const { data, isLoading, isError } = usePreparationQueue();
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

  if (isError) {
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
    <div className="flex flex-col gap-4 p-4 h-full min-h-0">
      <div className="flex items-center gap-2">
        <ChefHat className="w-5 h-5 text-primary" />
        <h1 className="text-base font-bold text-foreground">Màn hình bếp</h1>
      </div>

      <AlertsPanel alerts={data?.alerts ?? []} busy={actions.isPending} onAcknowledge={handleAcknowledge} />

      <CategoryFilterBar categories={categories} active={categoryFilter} onSelect={setCategoryFilter} />

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 flex-1 min-h-0">
        {COLUMN_KEYS.map((column) => (
          <QueueColumn
            key={column}
            column={column}
            tickets={board[column]}
            nowMs={nowMs}
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

      <KdsErrorToast message={errorMessage} onDismiss={() => setErrorMessage(null)} />
    </div>
  );
}
