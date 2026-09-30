import * as React from "react";

export interface ChangeTables {
  isOpen: boolean;
  open: () => void;
  close: () => void;
}

/**
 * Whether the change-tables dialog is open. It closes whenever another
 * Session takes over this terminal: a dialog left open for the previous party
 * would otherwise keep blocking hotkeys with nothing on screen to explain why.
 */
export function useChangeTables(activeSessionId: string | null): ChangeTables {
  const [isOpen, setIsOpen] = React.useState(false);

  React.useEffect(() => {
    // oxlint-disable-next-line react/set-state-in-effect
    setIsOpen(false);
  }, [activeSessionId]);

  return {
    isOpen,
    open: () => setIsOpen(true),
    close: () => setIsOpen(false),
  };
}
