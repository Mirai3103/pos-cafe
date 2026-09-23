import { useHotkeys } from "react-hotkeys-hook";
import type { PosPhase } from "../utils/phase";

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
}: PosHotkeysOptions): void {
  useHotkeys(
    "f9",
    (event) => {
      event.preventDefault();
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
