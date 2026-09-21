import type { ShiftCurrentShiftResponse } from "@/api/generated/models";

export interface CloseShiftDialogProps {
  shift: ShiftCurrentShiftResponse;
  isOpen: boolean;
  onClose: () => void;
}

export function CloseShiftDialog({ isOpen }: CloseShiftDialogProps) {
  if (!isOpen) return null;
  return null;
}
