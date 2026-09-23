import { useGetPreparationQueue } from "@/api/generated/endpoints/preparation/preparation";
import { unwrap } from "@/lib/unwrap";

/** The kitchen moves units; there is no push channel, so the board polls. */
export const PREPARATION_QUEUE_POLL_MS = 5_000;

/**
 * Reads the active Preparation Queue: units, alerts, and recent
 * Waste/Remake history in one consistent projection.
 */
export function usePreparationQueue() {
  return useGetPreparationQueue({
    query: {
      select: unwrap,
      refetchInterval: PREPARATION_QUEUE_POLL_MS,
    },
  });
}
