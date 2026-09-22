import { Link } from "@tanstack/react-router";
import { AlertTriangle, Clock } from "lucide-react";
import { Button } from "@/components/ui/button";

export function NoShiftNotice() {
  return (
    <div className="flex h-full flex-col items-center justify-center p-6 text-center gap-4 bg-muted/20">
      <div className="h-14 w-14 rounded-2xl bg-amber-500/10 text-amber-600 flex items-center justify-center border border-amber-200">
        <AlertTriangle className="h-7 w-7" />
      </div>
      <div className="space-y-1 max-w-xs">
        <h3 className="text-base font-bold text-foreground">Chưa có ca bán hàng mở</h3>
        <p className="text-xs text-muted-foreground leading-relaxed">
          Bạn vẫn có thể xem thực đơn, nhưng cần mở ca để bắt đầu tạo đơn và tính tiền.
        </p>
      </div>
      <Button render={<Link to="/shift" />} className="h-12 min-h-[48px] rounded-xl px-6 font-bold shadow-xs">
        <Clock className="h-4 w-4 mr-2" />
        Mở ca làm việc
      </Button>
    </div>
  );
}
