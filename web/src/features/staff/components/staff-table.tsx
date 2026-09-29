import type { ReactElement } from "react";
import { cn } from "@/lib/utils";
import { isSelf, ROLE_LABELS, type StaffRow } from "../lib/staff";

const ACTION =
  "h-12 min-h-[48px] rounded-xl border border-slate-200 bg-white px-3 text-xs font-bold text-slate-700 hover:bg-slate-100 dark:border-border dark:bg-card dark:text-foreground";

export interface StaffTableProps {
  rows: StaffRow[];
  selfId: string | null;
  onEdit: (row: StaffRow) => void;
  onResetPin: (row: StaffRow) => void;
  onToggle: (row: StaffRow) => void;
}

export function StaffTable({ rows, selfId, onEdit, onResetPin, onToggle }: StaffTableProps): ReactElement {
  if (rows.length === 0) {
    return <p className="rounded-2xl border border-dashed border-slate-300 p-8 text-center text-sm text-slate-500 dark:border-border">Không có nhân viên phù hợp</p>;
  }
  return (
    <div className="overflow-x-auto rounded-2xl border border-slate-200 bg-card dark:border-border">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase text-slate-500 dark:border-border dark:bg-muted">
          <tr>
            <th className="px-4 py-3">Tên</th>
            <th className="px-4 py-3">Mã</th>
            <th className="px-4 py-3">Vai trò</th>
            <th className="px-4 py-3">Trạng thái</th>
            <th className="px-4 py-3" />
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const self = isSelf(row, selfId);
            return (
              <tr key={row.id} className={cn("border-b border-slate-100 last:border-0 dark:border-border", !row.enabled && "opacity-60")}>
                <td className="px-4 py-3 font-semibold text-slate-900 dark:text-foreground">
                  {row.displayName}
                  {self && <span className="ml-2 rounded-full bg-emerald-100 px-2 py-0.5 text-[11px] font-bold text-emerald-700">Bạn</span>}
                </td>
                <td className="px-4 py-3 font-mono text-slate-700 dark:text-foreground">{row.loginCode}</td>
                <td className="px-4 py-3">
                  <div className="flex flex-wrap gap-1">
                    {row.roles.map((role) => (
                      <span key={role} className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] font-bold text-slate-700 dark:border-border dark:bg-muted dark:text-foreground">
                        {ROLE_LABELS[role]}
                      </span>
                    ))}
                  </div>
                </td>
                <td className="px-4 py-3 text-xs font-semibold">
                  {row.enabled ? <span className="text-emerald-700">● Hoạt động</span> : <span className="text-slate-500">○ Đã khóa</span>}
                </td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-2">
                    <button type="button" className={ACTION} onClick={() => onEdit(row)}>
                      Sửa
                    </button>
                    {!self && (
                      <>
                        <button type="button" className={ACTION} onClick={() => onResetPin(row)}>
                          Đặt lại PIN
                        </button>
                        <button type="button" className={ACTION} onClick={() => onToggle(row)}>
                          {row.enabled ? "Khóa" : "Mở khóa"}
                        </button>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
