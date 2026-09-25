import { useHotkeys } from "react-hotkeys-hook";
import type { PosPhase } from "../utils/phase";
import { resolveDineInF9, type DineInStatus } from "../utils/dine-in";

export type F9Action = "none" | "checkout" | "submit" | "next-customer" | "close";

/**
 * F9 always fires the panel's primary action; the labels in
 * components/check-panel-actions.tsx must match this table.
 */
export function resolveF9Action(phase: PosPhase, blocked: boolean): F9Action {
  if (blocked) return "none";
  switch (phase) {
    case "AWAITING_SUBMIT":
      return "submit";
    case "IN_PREPARATION":
      return "next-customer";
    case "READY_TO_CLOSE":
      return "close";
    default:
      return "checkout";
  }
}

export interface PosHotkeysOptions {
  phase: PosPhase;
  /** A dialog or the item picker is open, so the terminal keys belong to it. */
  blocked: boolean;
  isDrawerOpen: boolean;
  onCheckout: () => void;
  onSubmit: () => Promise<void>;
  onNextCustomer: () => void;
  onClose: () => Promise<void>;
  onToggleDrawer: () => void;
  /** Present while a dine-in Session is active: F9 follows the dine-in action bar. */
  dineIn?: {
    status: DineInStatus;
    onSend: () => Promise<void>;
    onCollect: () => void;
    onClose: () => Promise<void>;
  };
}

export function usePosHotkeys({
  phase,
  blocked,
  isDrawerOpen,
  onCheckout,
  onSubmit,
  onNextCustomer,
  onClose,
  onToggleDrawer,
  dineIn,
}: PosHotkeysOptions): void {
  useHotkeys(
    "f9",
    (event) => {
      event.preventDefault();
      if (dineIn) {
        switch (resolveDineInF9(dineIn.status, blocked || isDrawerOpen)) {
          case "send":
            void dineIn.onSend();
            break;
          case "collect":
            dineIn.onCollect();
            break;
          case "close":
            void dineIn.onClose();
            break;
          default:
            break;
        }
        return;
      }
      switch (resolveF9Action(phase, blocked || isDrawerOpen)) {
        case "checkout":
          onCheckout();
          break;
        case "submit":
          void onSubmit();
          break;
        case "next-customer":
          onNextCustomer();
          break;
        case "close":
          void onClose();
          break;
        default:
          break;
      }
    },
    { enableOnFormTags: true },
  );

  useHotkeys(
    "f4",
    (event) => {
      event.preventDefault();
      if (!blocked) onToggleDrawer();
    },
    { enableOnFormTags: true },
  );
}
