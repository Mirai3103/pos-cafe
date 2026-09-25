import { AlertCircle } from "lucide-react";

export function ErrorToast({ message, onDismiss }: { message: string | null; onDismiss: () => void }) {
  if (!message) return null;
  return (
    <div
      role="alert"
      className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 flex items-center gap-2 rounded-xl bg-destructive text-destructive-foreground px-4 py-2.5 text-xs font-bold shadow-lg animate-in fade-in slide-in-from-bottom-2"
    >
      <AlertCircle className="h-4 w-4 shrink-0" />
      <span>{message}</span>
      <button
        type="button"
        onClick={onDismiss}
        className="ml-2 text-destructive-foreground/80 hover:text-destructive-foreground underline min-h-[48px] px-2 flex items-center"
      >
        Đóng
      </button>
    </div>
  );
}
