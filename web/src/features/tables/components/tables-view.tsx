import * as React from "react";
import { useNavigate, Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  Grid2X2,
  LayoutGrid,
  PieChart,
  Plus,
  Power,
  RefreshCw,
  Table2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { useCurrentShift } from "@/features/shift/api/use-shift";
import { selectPosSession } from "@/features/pos/api/use-pos-session";
import { useSessionStore } from "@/stores/use-session-store";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { useTablesOverview, useTableAdmin } from "../api/use-tables";
import { floorStats, toFloorTables, type FloorTable } from "../lib/floor";
import { TableCard } from "./table-card";
import { OpenTableDialog } from "./open-table-dialog";
import { SessionPicker } from "./session-picker";
import { TableNameDialog, type TableNameTarget } from "./table-name-dialog";

export interface FloorGridProps {
  tables: FloorTable[];
  canAdminister: boolean;
  onOpen: (table: FloorTable) => void;
  onRename: (table: FloorTable) => void;
  onToggleAvailability: (table: FloorTable) => void;
  onCreate: () => void;
}

export function FloorGrid({
  tables,
  canAdminister,
  onOpen,
  onRename,
  onToggleAvailability,
  onCreate,
}: FloorGridProps) {
  if (tables.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
        <div className="w-12 h-12 rounded-2xl bg-muted flex items-center justify-center text-muted-foreground border border-border">
          <Grid2X2 className="h-6 w-6" />
        </div>
        <p className="text-sm font-semibold text-foreground">Chưa có bàn nào</p>
        {canAdminister && (
          <Button onClick={onCreate} className="h-12 min-h-[48px] rounded-xl font-bold">
            <Plus className="h-4 w-4 mr-2" /> Thêm bàn
          </Button>
        )}
      </div>
    );
  }
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-4">
      {tables.map((t) => (
        <TableCard
          key={t.id}
          table={t}
          canAdminister={canAdminister}
          onOpen={onOpen}
          onRename={onRename}
          onToggleAvailability={onToggleAvailability}
        />
      ))}
    </div>
  );
}

export function TablesView() {
  const navigate = useNavigate();
  const overview = useTablesOverview();
  const { data: shift } = useCurrentShift();
  const canAdminister = useSessionStore((s) => s.capabilities.includes("tables.administer"));
  const { setAvailability } = useTableAdmin();

  const [openTableId, setOpenTableId] = React.useState<string | null>(null);
  const [pickerTable, setPickerTable] = React.useState<FloorTable | null>(null);
  const [nameTarget, setNameTarget] = React.useState<TableNameTarget | null>(null);
  const [actionError, setActionError] = React.useState<string | null>(null);

  const isShiftOpen = shift?.state === "OPEN";
  const tables = toFloorTables(overview.data);
  const stats = floorStats(tables);
  const occupancyRate = stats.total > 0 ? Math.round((stats.occupied / stats.total) * 100) : 0;

  const enterSession = (sessionId: string) => {
    selectPosSession(sessionId);
    void navigate({ to: "/" });
  };

  const handleOpen = (table: FloorTable) => {
    setActionError(null);
    if (table.occupants.length === 1) return enterSession(table.occupants[0].sessionId);
    if (table.occupants.length > 1) return setPickerTable(table);
    if (!isShiftOpen) return;
    setOpenTableId(table.id);
  };

  const handleToggle = async (table: FloorTable) => {
    setActionError(null);
    try {
      await setAvailability(table.id, !table.available, newRequestId());
    } catch (err) {
      setActionError(messageForError(err));
    }
  };

  return (
    <div className="flex h-full flex-col gap-4 overflow-y-auto p-4 sm:p-6">
      {/* 1. Header Bar */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-emerald-50 dark:bg-emerald-950/60 text-emerald-700 dark:text-emerald-300 flex items-center justify-center border border-emerald-200/60 shadow-2xs">
            <Table2 className="w-5 h-5" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-base font-extrabold text-foreground tracking-tight">Sơ đồ bàn</h1>
              <span className="text-[11px] font-bold text-emerald-700 bg-emerald-50 dark:bg-emerald-950/60 dark:text-emerald-300 px-2 py-0.5 rounded-md border border-emerald-200/60">
                Phục vụ
              </span>
            </div>
            <p className="text-xs text-muted-foreground font-medium">Sơ đồ Bàn &amp; Điều phối Phục vụ</p>
          </div>
        </div>
        {canAdminister && tables.length > 0 && (
          <Button onClick={() => setNameTarget({ mode: "create" })} className="h-12 min-h-[48px] rounded-xl font-bold">
            <Plus className="h-4 w-4 mr-2" /> Thêm bàn
          </Button>
        )}
      </div>

      {/* 2. KPI Status Strip & Legend */}
      <div className="flex items-center gap-3 sm:gap-4 flex-wrap text-xs rounded-2xl border border-border bg-card p-3 shadow-2xs">
        {/* Available Count Pill */}
        <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-slate-50 dark:bg-slate-900/40 border border-dashed border-slate-300 dark:border-border text-slate-700 dark:text-slate-300">
          <span className="w-2.5 h-2.5 rounded-full bg-slate-300 border border-slate-400" />
          <span>Trống:</span>
          <strong className="font-mono font-bold text-foreground">{stats.free}</strong>
        </div>

        {/* Occupied Count Pill */}
        <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-indigo-50 dark:bg-indigo-950/40 border border-indigo-200 dark:border-indigo-800 text-indigo-900 dark:text-indigo-200">
          <span className="w-2.5 h-2.5 rounded-full bg-indigo-600" />
          <span>Đang ngồi:</span>
          <strong className="font-mono font-bold text-indigo-700 dark:text-indigo-300">{stats.occupied}</strong>
        </div>

        {/* Unavailable Count Pill */}
        <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-muted/40 border border-border text-muted-foreground">
          <Power className="w-3 h-3 text-muted-foreground" />
          <span>Tạm ngưng:</span>
          <strong className="font-mono font-bold text-foreground">{stats.unavailable}</strong>
        </div>

        {/* Occupancy Utilization Rate */}
        <div className="ml-auto hidden sm:flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-muted/40 border border-border text-muted-foreground font-medium">
          <PieChart className="w-3.5 h-3.5 text-muted-foreground" />
          <span>Công suất:</span>
          <strong className="font-mono font-bold text-foreground">
            {`${occupancyRate}% (${stats.occupied}/${stats.total})`}
          </strong>
        </div>
      </div>

      {!isShiftOpen && (
        <div
          role="status"
          className="flex flex-wrap items-center gap-3 rounded-2xl border border-amber-200 bg-amber-50 dark:bg-amber-950/40 px-4 py-3 text-sm text-amber-900 dark:text-amber-200"
        >
          <AlertTriangle className="h-5 w-5 text-amber-600 flex-shrink-0" />
          <span className="flex-1 font-medium">Chưa có ca bán hàng mở. Cần mở ca để nhận khách tại bàn.</span>
          <Button render={<Link to="/shift" />} variant="outline" className="h-12 min-h-[48px] rounded-xl border-amber-300 hover:bg-amber-100 dark:hover:bg-amber-900/60 font-bold">
            Mở ca làm việc
          </Button>
        </div>
      )}

      {actionError && <p role="alert" className="text-sm font-semibold text-destructive">{actionError}</p>}

      {/* 3. Section Title & Dynamic Counter */}
      <div className="flex items-center justify-between text-xs text-muted-foreground px-1">
        <div className="flex items-center gap-2">
          <LayoutGrid className="w-4 h-4 text-muted-foreground" />
          <span className="font-bold text-foreground uppercase tracking-wider text-[11px]">Sơ đồ tất cả bàn</span>
        </div>
        <span className="font-medium text-muted-foreground">{`Đang hiển thị ${tables.length} bàn`}</span>
      </div>

      {overview.isLoading ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-4">
          {Array.from({ length: 10 }, (_, i) => (
            <div key={i} className="min-h-[175px] rounded-2xl bg-muted/50 border border-border animate-pulse" />
          ))}
        </div>
      ) : overview.isError ? (
        <div className="flex flex-col items-center gap-3 py-16 text-center">
          <p className="text-sm font-semibold text-destructive">{messageForError(overview.error)}</p>
          <Button variant="outline" onClick={() => void overview.refetch()} className="h-12 min-h-[48px] rounded-xl">
            <RefreshCw className="h-4 w-4 mr-2" /> Thử lại
          </Button>
        </div>
      ) : (
        <FloorGrid
          tables={tables}
          canAdminister={canAdminister}
          onOpen={handleOpen}
          onRename={(table) => setNameTarget({ mode: "rename", table })}
          onToggleAvailability={(table) => void handleToggle(table)}
          onCreate={() => setNameTarget({ mode: "create" })}
        />
      )}

      <OpenTableDialog
        initialTableId={openTableId}
        tables={tables}
        onClose={() => setOpenTableId(null)}
        onOpened={enterSession}
      />
      <SessionPicker
        table={pickerTable}
        canOpenNew={isShiftOpen}
        onPick={enterSession}
        onOpenNew={(table) => {
          setPickerTable(null);
          setOpenTableId(table.id);
        }}
        onClose={() => setPickerTable(null)}
      />
      <TableNameDialog target={nameTarget} onClose={() => setNameTarget(null)} />
    </div>
  );
}
