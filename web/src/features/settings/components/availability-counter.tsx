import type { ReactElement } from "react";
import { useAvailabilityMenu } from "../api/use-availability";
import { toAvailabilityView } from "../lib/availability";

export function TabCounter({ count }: { count: number }): ReactElement | null {
  if (count === 0) return null;
  return <span className="rounded-full bg-rose-600 px-2 py-0.5 font-mono text-2xs font-bold text-white">{`${count} tạm hết`}</span>;
}

/** Shares the tab's query cache, so it costs no extra request. */
export function AvailabilityCounter(): ReactElement | null {
  const { data } = useAvailabilityMenu();
  return <TabCounter count={toAvailabilityView(data).stats.unavailable} />;
}
