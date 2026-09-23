import { Clock, AlertCircle, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";

export function PosMenuLoading() {
  return (
    <div className="flex flex-col items-center justify-center p-12 min-h-[60vh] gap-4">
      <div className="w-12 h-12 rounded-2xl bg-primary/10 text-primary flex items-center justify-center animate-pulse">
        <Clock className="w-6 h-6 animate-spin" />
      </div>
      <p className="text-sm text-muted-foreground font-medium">Đang tải thực đơn bán hàng...</p>
    </div>
  );
}

export function PosMenuError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center p-8 max-w-md mx-auto min-h-[60vh] text-center gap-4">
      <div className="w-12 h-12 rounded-2xl bg-destructive/10 text-destructive flex items-center justify-center">
        <AlertCircle className="w-6 h-6" />
      </div>
      <div className="space-y-1">
        <h3 className="text-base font-bold text-foreground">Không thể tải thực đơn</h3>
        <p className="text-xs text-muted-foreground">{message}</p>
      </div>
      <Button
        type="button"
        variant="outline"
        onClick={onRetry}
        className="rounded-xl h-10 min-h-[48px] px-6"
      >
        <RefreshCw className="w-4 h-4 mr-2" />
        Thử lại
      </Button>
    </div>
  );
}
