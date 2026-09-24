import * as React from "react";
import { useNavigate, Link } from "@tanstack/react-router";
import { AlertTriangle, Grid2X2, Plus, RefreshCw } from "lucide-react";
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

export function FloorGrid({ tables, canAdminister, onOpen, onRename, onToggleAvailability, onCreate }: FloorGridProps) {
  if (tables.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
        <Grid2X2 className="h-8 w-8 text-muted-foreground" />
        <p className="text-sm font-semibold">Chưa có bàn nào</p>
        {canAdminister && (
          <Button onClick={onCreate} className="h-12 min-h-[48px] rounded-xl font-bold">
            <Plus className="h-4 w-4 mr-2" /> Thêm bàn
          </Button>
        )}
      </div>
    );
  }
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-3">
      {tables.map((t) => (
        <TableCard key={t.id} table={t} canAdminister={canAdminister} onOpen={onOpen} onRename={onRename} onToggleAvailability={onToggleAvailability} />
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
    <div className="flex h-full flex-col gap-4 overflow-y-auto p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Sơ đồ bàn</h1>
          <p className="text-xs text-muted-foreground">
            {`${stats.total} bàn · ${stats.free} trống · ${stats.occupied} có khách · ${stats.unavailable} tạm ngưng`}
          </p>
        </div>
        {canAdminister && tables.length > 0 && (
          <Button onClick={() => setNameTarget({ mode: "create" })} className="h-12 min-h-[48px] rounded-xl font-bold">
            <Plus className="h-4 w-4 mr-2" /> Thêm bàn
          </Button>
        )}
      </div>

      {!isShiftOpen && (
        <div role="status" className="flex flex-wrap items-center gap-3 rounded-xl border border-amber-200 bg-amber-50 dark:bg-amber-950/40 px-4 py-3 text-sm">
          <AlertTriangle className="h-4 w-4 text-amber-600" />
          <span className="flex-1">Chưa có ca bán hàng mở. Cần mở ca để nhận khách tại bàn.</span>
          <Button render={<Link to="/shift" />} variant="outline" className="h-12 min-h-[48px] rounded-xl">Mở ca làm việc</Button>
        </div>
      )}

      {actionError && <p role="alert" className="text-sm font-semibold text-destructive">{actionError}</p>}

      {overview.isLoading ? (
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-3">
          {Array.from({ length: 8 }, (_, i) => (
            <div key={i} className="min-h-[120px] rounded-2xl bg-muted animate-pulse" />
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

      <OpenTableDialog initialTableId={openTableId} tables={tables} onClose={() => setOpenTableId(null)} onOpened={enterSession} />
      <SessionPicker
        table={pickerTable}
        canOpenNew={isShiftOpen}
        onPick={enterSession}
        onOpenNew={(table) => { setPickerTable(null); setOpenTableId(table.id); }}
        onClose={() => setPickerTable(null)}
      />
      <TableNameDialog target={nameTarget} onClose={() => setNameTarget(null)} />
    </div>
  );
}
