import type { TablesTableOverviewRow } from "@/api/generated/models";

export type TableCardState = "FREE" | "OCCUPIED" | "UNAVAILABLE";

export interface FloorOccupant {
  sessionId: string;
  serviceNumber: string;
}

export interface FloorTable {
  id: string;
  name: string;
  available: boolean;
  occupants: FloorOccupant[];
  state: TableCardState;
}

/**
 * A Table still serving a party reads as occupied even after it is switched
 * off: the party is still there. Unavailability only stops new assignments.
 */
export function tableCardState(row: TablesTableOverviewRow): TableCardState {
  if ((row.current_service_sessions?.length ?? 0) > 0) return "OCCUPIED";
  return row.available ? "FREE" : "UNAVAILABLE";
}

export function toFloorTables(rows: TablesTableOverviewRow[] | null | undefined): FloorTable[] {
  return (rows ?? [])
    .filter((row) => Boolean(row.id))
    .map((row) => ({
      id: row.id!,
      name: row.name ?? "",
      available: row.available === true,
      occupants: (row.current_service_sessions ?? [])
        .filter((o) => Boolean(o.service_session_id))
        .map((o) => ({ sessionId: o.service_session_id!, serviceNumber: o.service_number ?? "" })),
      state: tableCardState(row),
    }));
}

export function floorStats(tables: FloorTable[]) {
  return {
    total: tables.length,
    free: tables.filter((t) => t.state === "FREE").length,
    occupied: tables.filter((t) => t.state === "OCCUPIED").length,
    unavailable: tables.filter((t) => t.state === "UNAVAILABLE").length,
  };
}

export function toggleSelection(ids: string[], id: string): string[] {
  return ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id];
}
