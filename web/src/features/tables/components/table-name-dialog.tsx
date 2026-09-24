import { useRef, useState, type ReactElement } from "react";
import { Pencil, Plus, X } from "lucide-react";
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

  const isCreate = target.mode === "create";
  const title = isCreate ? "Thêm bàn mới" : "Đổi tên bàn";
  const subtitle = isCreate ? "Thiết lập tên bàn hiển thị trên sơ đồ" : "Cập nhật tên bàn hiển thị";

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4 backdrop-blur-xs">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="table-name-title"
        className="flex w-full max-w-sm flex-col gap-4 rounded-3xl border border-border bg-card p-6 shadow-2xl"
      >
        <div className="flex items-center justify-between pb-3 border-b border-border">
          <div className="flex items-center gap-2.5">
            <div className="w-10 h-10 rounded-xl bg-slate-100 dark:bg-muted text-foreground flex items-center justify-center border border-border">
              {isCreate ? <Plus className="w-5 h-5" /> : <Pencil className="w-5 h-5" />}
            </div>
            <div>
              <h2 id="table-name-title" className="text-base font-bold text-foreground">
                {title}
              </h2>
              <p className="text-xs text-muted-foreground">{subtitle}</p>
            </div>
          </div>
          <button
            type="button"
            aria-label="Đóng"
            onClick={onClose}
            disabled={isPending}
            className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-xl flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted transition"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="space-y-1.5">
          <label className="block text-xs font-bold text-muted-foreground uppercase tracking-wider">
            Tên bàn:
          </label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Ví dụ: Bàn 09"
            className="h-12 rounded-xl text-sm font-semibold"
            autoFocus
          />
          {error && <p role="alert" className="text-xs font-semibold text-destructive">{error}</p>}
        </div>

        <div className="pt-2 border-t border-border flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            disabled={isPending}
            className="min-h-[48px] h-12 px-4 rounded-xl border border-border bg-card hover:bg-muted text-xs font-bold text-foreground transition"
          >
            Hủy bỏ
          </button>
          <Button
            onClick={handleSave}
            disabled={isPending || !name.trim()}
            className="min-h-[48px] h-12 px-6 rounded-xl font-bold"
          >
            {isPending ? "Đang lưu..." : "Lưu"}
          </Button>
        </div>
      </div>
    </div>
  );
}
