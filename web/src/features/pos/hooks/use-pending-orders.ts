import * as React from "react";
import { messageForError } from "@/lib/error-messages";
import { useActiveSessions } from "../api/use-pos";
import { countReadyToClose, toPendingOrders, type PendingOrder } from "../lib/pending-orders";

export interface PendingOrders {
  isOpen: boolean;
  open: () => void;
  close: () => void;
  toggle: () => void;
  orders: PendingOrder[];
  readyCount: number;
  isLoading: boolean;
  errorMessage: string | null;
  /** When the list was last fetched; ages are measured against it. */
  nowMs: number;
  retry: () => void;
}

/**
 * The "Đơn đang chờ" drawer: whether it is open, and the active Sessions it
 * lists (polled faster while the cashier is looking at it).
 */
export function usePendingOrders(): PendingOrders {
  const [isOpen, setIsOpen] = React.useState(false);
  const activeSessions = useActiveSessions(isOpen);
  const orders = toPendingOrders(activeSessions.data);

  return {
    isOpen,
    open: () => setIsOpen(true),
    close: () => setIsOpen(false),
    toggle: () => setIsOpen((open) => !open),
    orders,
    readyCount: countReadyToClose(orders),
    isLoading: activeSessions.isLoading,
    errorMessage: activeSessions.isError ? messageForError(activeSessions.error) : null,
    nowMs: activeSessions.dataUpdatedAt,
    retry: () => void activeSessions.refetch(),
  };
}
