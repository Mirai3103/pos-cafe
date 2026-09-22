import { ShoppingBag } from "lucide-react";

export function DraftEmptyState() {
  return (
    <div className="flex h-full flex-col items-center justify-center p-6 text-center text-muted-foreground gap-3">
      <div className="h-16 w-16 rounded-2xl bg-muted/40 border border-border flex items-center justify-center text-muted-foreground/60 shadow-2xs">
        <ShoppingBag className="h-8 w-8" />
      </div>
      <div className="space-y-1">
        <p className="text-sm font-bold text-foreground">Chưa có món nào trong đơn</p>
        <p className="text-xs text-muted-foreground">
          Chạm vào món từ thực đơn bên trái để thêm vào đơn mang đi
        </p>
      </div>
    </div>
  );
}
