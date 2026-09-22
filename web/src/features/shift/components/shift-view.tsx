import { useState } from "react";
import { AlertCircle, RefreshCw, Clock } from "lucide-react";
import { useCurrentShift } from "@/features/shift/api/use-shift";
import { NoActiveShiftView } from "./no-active-shift-view";
import { OpenShiftDialog } from "./open-shift-dialog";
import { OpenShiftDashboard } from "./open-shift-dashboard";
import { ClosingReconciliationView } from "./closing-reconciliation-view";
import { Button } from "@/components/ui/button";

export function ShiftView() {
  const { data: shift, isLoading, isError, error, refetch } = useCurrentShift();
  const [openShiftModalOpen, setOpenShiftModalOpen] = useState(false);

  if (isLoading) {
    return (
      <div className="flex flex-col items-center justify-center p-12 min-h-[60vh] gap-4">
        <div className="w-12 h-12 rounded-2xl bg-primary/10 text-primary flex items-center justify-center animate-pulse">
          <Clock className="w-6 h-6 animate-spin" />
        </div>
        <p className="text-sm text-muted-foreground font-medium">Đang tải thông tin ca làm việc...</p>
      </div>
    );
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center p-8 max-w-md mx-auto min-h-[60vh] text-center gap-4">
        <div className="w-12 h-12 rounded-2xl bg-destructive/10 text-destructive flex items-center justify-center">
          <AlertCircle className="w-6 h-6" />
        </div>
        <div className="space-y-1">
          <h3 className="text-base font-bold text-foreground">Không thể tải thông tin ca</h3>
          <p className="text-xs text-muted-foreground">{(error as { message?: string } | null)?.message || "Đã xảy ra lỗi kết nối"}</p>
        </div>
        <Button type="button" variant="outline" onClick={() => refetch()} className="rounded-xl h-10">
          <RefreshCw className="w-4 h-4 mr-2" />
          Thử lại
        </Button>
      </div>
    );
  }

  return (
    <div className="min-h-full pb-12">
      {/* State Router */}
      {!shift ? (
        <>
          <NoActiveShiftView onOpen={() => setOpenShiftModalOpen(true)} />
          <OpenShiftDialog
            isOpen={openShiftModalOpen}
            onClose={() => setOpenShiftModalOpen(false)}
          />
        </>
      ) : shift.state === "OPEN" ? (
        <OpenShiftDashboard shift={shift} />
      ) : shift.state === "CLOSING" ? (
        <ClosingReconciliationView shift={shift} />
      ) : (
        <NoActiveShiftView onOpen={() => setOpenShiftModalOpen(true)} />
      )}
    </div>
  );
}
