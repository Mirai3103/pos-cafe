import { useState, type ReactElement } from "react";
import { Plus, Search } from "lucide-react";
import { useSessionStore } from "@/stores/use-session-store";
import { useStaffList } from "../api/use-staff";
import { filterStaff, type StaffRow } from "../lib/staff";
import { ResetPinModal } from "./reset-pin-modal";
import { StaffFormModal } from "./staff-form-modal";
import { StaffTable } from "./staff-table";
import { ToggleEnabledDialog } from "./toggle-enabled-dialog";

/** The tab's badge: enabled staff. Shares the tab's query cache. */
export function StaffCounter(): ReactElement {
  const { rows } = useStaffList();
  return (
    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 font-mono text-[11px] font-bold text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-400">
      {`${rows.filter((r) => r.enabled).length} người`}
    </span>
  );
}

type FormState = { open: false } | { open: true; row: StaffRow | null; key: number };

export function StaffView(): ReactElement {
  const { rows, isPending, error, refetch } = useStaffList();
  const selfId = useSessionStore((s) => s.staffId);
  const [query, setQuery] = useState("");
  const [showDisabled, setShowDisabled] = useState(false);
  const [form, setForm] = useState<FormState>({ open: false });
  const [resetting, setResetting] = useState<StaffRow | null>(null);
  const [toggling, setToggling] = useState<StaffRow | null>(null);
  const openForm = (row: StaffRow | null) => setForm({ open: true, row, key: Date.now() });

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-4 p-4 sm:p-6">
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={() => openForm(null)}
          className="flex h-12 min-h-[48px] items-center gap-2 rounded-xl bg-emerald-600 px-5 text-sm font-bold text-white hover:bg-emerald-700"
        >
          <Plus className="h-4 w-4" />
          Thêm nhân viên
        </button>
        <label className="flex h-12 min-h-[48px] flex-1 items-center gap-2 rounded-xl border border-slate-300 bg-card px-3 dark:border-border">
          <Search className="h-4 w-4 text-slate-400" />
          <input
            aria-label="Tìm nhân viên"
            placeholder="Tìm theo tên hoặc mã…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="w-full bg-transparent text-sm outline-none"
          />
        </label>
        <label className="flex min-h-[48px] items-center gap-2 text-sm font-semibold text-slate-700 dark:text-foreground">
          <input type="checkbox" checked={showDisabled} onChange={(e) => setShowDisabled(e.target.checked)} className="h-5 w-5 accent-emerald-600" />
          Hiện tài khoản đã khóa
        </label>
      </div>

      {error ? (
        <div role="alert" className="flex items-center justify-between rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
          {error.message}
          <button type="button" onClick={refetch} className="min-h-[48px] font-bold underline">
            Thử lại
          </button>
        </div>
      ) : isPending ? (
        <p className="p-8 text-center text-sm text-slate-500">Đang tải…</p>
      ) : (
        <StaffTable rows={filterStaff(rows, query, showDisabled)} selfId={selfId} onEdit={openForm} onResetPin={setResetting} onToggle={setToggling} />
      )}

      {form.open && <StaffFormModal key={form.key} open row={form.row} onClose={() => setForm({ open: false })} />}
      <ResetPinModal row={resetting} onClose={() => setResetting(null)} />
      <ToggleEnabledDialog row={toggling} onClose={() => setToggling(null)} />
    </div>
  );
}
