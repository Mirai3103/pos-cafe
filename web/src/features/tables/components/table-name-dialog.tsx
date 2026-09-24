import { useRef, useState, type ReactElement } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { ApiError } from "@/lib/unwrap";
import { useTableAdmin } from "../api/use-tables";
import type { FloorTable } from "../lib/floor";

export type TableNameTarget = { mode: "create" } | { mode: "rename"; table: FloorTable };

export interface TableNameDialogProps {
  target: TableNameTarget | null;
  onClose: () => void;
}

export function TableNameDialog({ target, onClose }: TableNameDialogProps): ReactElement | null {
  if (!target) return null;
  const key = target.mode === "create" ? "create" : `rename-${target.table.id}`;
  return <TableNameForm key={key} target={target} onClose={onClose} />;
}

function TableNameForm({ target, onClose }: { target: TableNameTarget; onClose: () => void }) {
  const { createTable, renameTable, isPending } = useTableAdmin();
  const [name, setName] = useState(target.mode === "rename" ? target.table.name : "");
  const [error, setError] = useState<string | null>(null);
  const requestIdRef = useRef(newRequestId());

  async function handleSave() {
    const trimmed = name.trim();
    if (isPending || !trimmed) return;
    setError(null);
    try {
      if (target.mode === "create") await createTable(trimmed, requestIdRef.current);
      else await renameTable(target.table.id, trimmed, requestIdRef.current);
      onClose();
    } catch (err) {
      setError(messageForError(err));
      // The name was refused, so the next attempt carries a different name.
      if (err instanceof ApiError && err.code === "TABLE_NAME_CONFLICT") requestIdRef.current = newRequestId();
    }
  }

  const title = target.mode === "create" ? "Thêm bàn" : "Đổi tên bàn";
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-xs">
      <div role="dialog" aria-modal="true" aria-labelledby="table-name-title" className="flex w-full max-w-sm flex-col gap-4 rounded-2xl border border-border bg-card p-6 shadow-xl">
        <div className="flex items-start justify-between">
          <h2 id="table-name-title" className="text-base font-bold">{title}</h2>
          <button type="button" aria-label="Đóng" onClick={onClose} disabled={isPending} className="h-12 w-12 rounded-xl flex items-center justify-center hover:bg-muted">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="space-y-1">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Ví dụ: Bàn 9" className="h-12" autoFocus />
          {error && <p role="alert" className="text-xs font-semibold text-destructive">{error}</p>}
        </div>
        <Button onClick={handleSave} disabled={isPending || !name.trim()} className="h-12 min-h-[48px] rounded-xl font-bold">
          {isPending ? "Đang lưu..." : "Lưu"}
        </Button>
      </div>
    </div>
  );
}
