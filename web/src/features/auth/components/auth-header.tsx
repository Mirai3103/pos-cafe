import * as React from "react";
import { LayoutGrid, Volume2, VolumeX } from "lucide-react";
import { useSound } from "@/lib/sound";

export interface AuthHeaderProps {
  stationSubtitle?: string;
}

export function AuthHeader({ stationSubtitle = "Đăng nhập PIN" }: AuthHeaderProps) {
  const { enabled: soundEnabled, toggle: toggleSound } = useSound();
  const [time, setTime] = React.useState<string>("");
  const [date, setDate] = React.useState<string>("");

  React.useEffect(() => {
    const update = () => {
      const now = new Date();
      setTime(now.toLocaleTimeString("vi-VN", { hour12: false }));
      setDate(
        now.toLocaleDateString("vi-VN", {
          weekday: "short",
          day: "2-digit",
          month: "2-digit",
          year: "numeric",
        }),
      );
    };
    update();
    const timer = setInterval(update, 1000);
    return () => clearInterval(timer);
  }, []);

  return (
    <header className="relative z-50 flex h-16 w-full shrink-0 select-none items-center justify-between border-b border-border bg-card/95 px-4 backdrop-blur sm:px-6">
      {/* Left: Station Brand & Store Tagline */}
      <div className="flex items-center gap-3">
        <div className="flex h-12 min-h-[48px] items-center gap-2 rounded-xl border border-border bg-card px-3 shadow-xs">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-primary/20 bg-primary/10 text-primary">
            <LayoutGrid className="size-4" />
          </div>
          <div className="flex flex-col text-left">
            <span className="text-xs font-bold leading-tight text-foreground">Trạm làm việc</span>
            <span className="text-[10px] font-medium leading-tight text-primary">{stationSubtitle}</span>
          </div>
        </div>

        <div className="hidden items-center gap-2 border-l border-border pl-3 md:flex">
          <span className="text-xs font-semibold text-foreground">The Coffee Workshop</span>
          <span className="text-[11px] font-medium text-muted-foreground">· Chi nhánh Tây Hồ</span>
        </div>
      </div>

      {/* Right: Sound Toggle & Clock */}
      <div className="flex items-center gap-2.5 sm:gap-3">
        <button
          type="button"
          onClick={toggleSound}
          className={`flex h-12 w-12 min-h-[48px] min-w-[48px] cursor-pointer items-center justify-center rounded-xl border transition select-none active:scale-95 focus:outline-none focus:ring-2 ${
            soundEnabled
              ? "border-emerald-200 bg-emerald-50/70 text-emerald-700 hover:bg-emerald-100/70 focus:ring-emerald-500/20 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-400"
              : "border-border bg-muted text-muted-foreground hover:bg-muted/80 focus:ring-muted"
          }`}
          title={soundEnabled ? "Âm thanh: Đang bật (Click để tắt)" : "Âm thanh: Đang tắt (Click để bật)"}
          aria-label="Bật/tắt âm thanh"
          aria-pressed={soundEnabled}
        >
          {soundEnabled ? <Volume2 className="size-5" /> : <VolumeX className="size-5" />}
        </button>

        <div className="hidden flex-col items-end rounded-xl border border-border bg-card px-3 py-1.5 shadow-xs sm:flex">
          <span className="font-mono text-xs font-bold tracking-tight text-foreground">
            {time || "--:--:--"}
          </span>
          <span className="text-[10px] font-medium text-muted-foreground">{date || "--/--/----"}</span>
        </div>
      </div>
    </header>
  );
}
