import { useHotkeys } from "react-hotkeys-hook";

export interface UseKeypadHotkeysOptions {
  /**
   * Called when a numeric digit (0-9) is pressed.
   * Not triggered when activeElement is an input/textarea/select unless enableOnFormTags is true.
   */
  onDigit?: (digit: string) => void;

  /**
   * Called when Backspace is pressed.
   * Not triggered when activeElement is an input/textarea/select unless enableOnFormTags is true.
   */
  onBackspace?: () => void;

  /**
   * Called when 'c' or 'C' is pressed (and 'Escape' if onClose is not specified).
   * Not triggered when activeElement is an input/textarea/select unless enableOnFormTags is true.
   */
  onClear?: () => void;

  /**
   * Called when Enter is pressed.
   * Enabled on form tags by default so pressing Enter in an input triggers submit.
   */
  onSubmit?: () => void;

  /**
   * Called when Escape is pressed (e.g. to close a modal or dialog).
   * Enabled on form tags by default so pressing Escape cancels even if an input is focused.
   */
  onClose?: () => void;

  /**
   * Whether hotkey listeners are enabled. Defaults to true.
   */
  enabled?: boolean;

  /**
   * Whether to capture digit/backspace/clear keys even when focused inside form tags.
   * Defaults to false, allowing natural typing in inputs (e.g. login code).
   */
  enableOnFormTags?: boolean;
}

const DIGITS = ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9"] as const;

export function useKeypadHotkeys({
  onDigit,
  onBackspace,
  onClear,
  onSubmit,
  onClose,
  enabled = true,
  enableOnFormTags = false,
}: UseKeypadHotkeysOptions) {
  // 1. Digits 0-9
  useHotkeys(
    DIGITS,
    (e) => {
      onDigit?.(e.key);
    },
    {
      enabled: enabled && !!onDigit,
      enableOnFormTags,
      preventDefault: true,
    },
    [onDigit, enabled, enableOnFormTags]
  );

  // 2. Backspace
  useHotkeys(
    "backspace",
    () => {
      onBackspace?.();
    },
    {
      enabled: enabled && !!onBackspace,
      enableOnFormTags,
      preventDefault: true,
    },
    [onBackspace, enabled, enableOnFormTags]
  );

  // 3. Clear (c, C)
  useHotkeys(
    ["c", "shift+c"],
    () => {
      onClear?.();
    },
    {
      enabled: enabled && !!onClear,
      enableOnFormTags,
      preventDefault: true,
    },
    [onClear, enabled, enableOnFormTags]
  );

  // 4. Escape:
  // - If onClose is provided, Escape triggers onClose (always enabled on form tags)
  // - Otherwise, if onClear is provided, Escape triggers onClear
  useHotkeys(
    "escape",
    () => {
      if (onClose) {
        onClose();
      } else if (onClear) {
        onClear();
      }
    },
    {
      enabled: enabled && (!!onClose || !!onClear),
      enableOnFormTags: true,
      preventDefault: true,
    },
    [onClose, onClear, enabled]
  );

  // 5. Enter: triggers onSubmit (always enabled on form tags)
  useHotkeys(
    "enter",
    () => {
      onSubmit?.();
    },
    {
      enabled: enabled && !!onSubmit,
      enableOnFormTags: true,
      preventDefault: true,
    },
    [onSubmit, enabled]
  );
}
