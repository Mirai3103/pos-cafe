import { Clock, Play, WalletCards } from "lucide-react";
import { Button } from "@/components/ui/button";

interface NoActiveShiftViewProps {
  onOpen: () => void;
}

export function NoActiveShiftView({ onOpen }: NoActiveShiftViewProps) {
  return (
    <div className="flex flex-col items-center justify-center p-8 text-center max-w-lg mx-auto min-h-[60vh] gap-6 animate-in fade-in duration-300">
      <div className="w-20 h-20 rounded-3xl bg-primary/10 text-primary flex items-center justify-center shadow-inner">
        <Clock className="w-10 h-10 stroke-[1.75]" />
      </div>

      <div className="space-y-2">
        <h2 className="text-xl font-bold text-foreground">Chưa có ca làm việc nào đang mở</h2>
        <p className="text-sm text-muted-foreground leading-relaxed">
          Quầy thu ngân cần mở ca làm việc và ghi nhận số tiền mặt đầu ca (Opening Float) trước khi có thể thực hiện thanh toán đơn hàng.
        </p>
      </div>

      <div className="p-4 rounded-xl border border-border bg-card/60 flex items-center gap-3 text-left w-full">
        <WalletCards className="w-5 h-5 text-primary shrink-0" />
        <div className="text-xs text-muted-foreground">
          <span className="font-semibold text-foreground block">Nguyên tắc kiểm đếm mù:</span>
          Số tiền đầu ca sẽ được lưu an toàn trên máy chủ để đối soát kết ca.
        </div>
      </div>

      <Button
        type="button"
        onClick={onOpen}
        className="min-h-12 h-12 px-8 rounded-xl text-base font-bold shadow-md hover:shadow-lg transition active:scale-98"
      >
        <Play className="w-5 h-5 fill-current mr-2" />
        Mở ca làm việc ngay
      </Button>
    </div>
  );
}
