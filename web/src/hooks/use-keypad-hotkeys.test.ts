import { describe, it, expect, mock, beforeEach } from "bun:test";

interface RegisteredHotkey {
  keys: unknown;
  callback: (e: { key?: string }) => void;
  options: {
    enabled?: boolean;
    enableOnFormTags?: boolean;
    preventDefault?: boolean;
  };
}

const registeredHotkeys: RegisteredHotkey[] = [];

mock.module("react-hotkeys-hook", () => ({
  useHotkeys: (
    keys: unknown,
    callback: (e: { key?: string }) => void,
    options: RegisteredHotkey["options"] = {}
  ) => {
    registeredHotkeys.push({ keys, callback, options });
  },
}));

// Import after mocking
const { useKeypadHotkeys } = await import("./use-keypad-hotkeys");

describe("useKeypadHotkeys", () => {
  beforeEach(() => {
    registeredHotkeys.length = 0;
  });

  it("registers all 5 hotkey categories with correct options", () => {
    const onDigit = mock();
    const onBackspace = mock();
    const onClear = mock();
    const onSubmit = mock();
    const onClose = mock();

    useKeypadHotkeys({
      onDigit,
      onBackspace,
      onClear,
      onSubmit,
      onClose,
      enabled: true,
    });

    expect(registeredHotkeys.length).toBe(5);

    // 1. Digits
    const digitHotkey = registeredHotkeys.find(
      (h) => Array.isArray(h.keys) && h.keys.includes("0") && h.keys.includes("9")
    );
    expect(digitHotkey).toBeDefined();
    expect(digitHotkey?.options.enabled).toBe(true);
    expect(digitHotkey?.options.enableOnFormTags).toBe(false);
    expect(digitHotkey?.options.preventDefault).toBe(true);

    // 2. Backspace
    const backspaceHotkey = registeredHotkeys.find((h) => h.keys === "backspace");
    expect(backspaceHotkey).toBeDefined();
    expect(backspaceHotkey?.options.enabled).toBe(true);
    expect(backspaceHotkey?.options.enableOnFormTags).toBe(false);

    // 3. Clear (c, shift+c)
    const clearHotkey = registeredHotkeys.find(
      (h) => Array.isArray(h.keys) && h.keys.includes("c")
    );
    expect(clearHotkey).toBeDefined();
    expect(clearHotkey?.options.enabled).toBe(true);
    expect(clearHotkey?.options.enableOnFormTags).toBe(false);

    // 4. Escape
    const escapeHotkey = registeredHotkeys.find((h) => h.keys === "escape");
    expect(escapeHotkey).toBeDefined();
    expect(escapeHotkey?.options.enabled).toBe(true);
    expect(escapeHotkey?.options.enableOnFormTags).toBe(true);

    // 5. Enter
    const enterHotkey = registeredHotkeys.find((h) => h.keys === "enter");
    expect(enterHotkey).toBeDefined();
    expect(enterHotkey?.options.enabled).toBe(true);
    expect(enterHotkey?.options.enableOnFormTags).toBe(true);
  });

  it("triggers onDigit callback when digit key is pressed", () => {
    const onDigit = mock();
    useKeypadHotkeys({ onDigit });

    const digitHotkey = registeredHotkeys.find(
      (h) => Array.isArray(h.keys) && h.keys.includes("0")
    );
    expect(digitHotkey).toBeDefined();

    digitHotkey?.callback({ key: "7" });
    expect(onDigit).toHaveBeenCalledWith("7");
  });

  it("triggers onBackspace callback when backspace key is pressed", () => {
    const onBackspace = mock();
    useKeypadHotkeys({ onBackspace });

    const backspaceHotkey = registeredHotkeys.find((h) => h.keys === "backspace");
    expect(backspaceHotkey).toBeDefined();

    backspaceHotkey?.callback({});
    expect(onBackspace).toHaveBeenCalledTimes(1);
  });

  it("triggers onClear callback when c key is pressed", () => {
    const onClear = mock();
    useKeypadHotkeys({ onClear });

    const clearHotkey = registeredHotkeys.find(
      (h) => Array.isArray(h.keys) && h.keys.includes("c")
    );
    expect(clearHotkey).toBeDefined();

    clearHotkey?.callback({});
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it("triggers onClose on Escape when onClose is provided", () => {
    const onClose = mock();
    const onClear = mock();
    useKeypadHotkeys({ onClose, onClear });

    const escapeHotkey = registeredHotkeys.find((h) => h.keys === "escape");
    expect(escapeHotkey).toBeDefined();

    escapeHotkey?.callback({});
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onClear).not.toHaveBeenCalled();
  });

  it("falls back to onClear on Escape when onClose is not provided", () => {
    const onClear = mock();
    useKeypadHotkeys({ onClear });

    const escapeHotkey = registeredHotkeys.find((h) => h.keys === "escape");
    expect(escapeHotkey).toBeDefined();

    escapeHotkey?.callback({});
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it("triggers onSubmit when enter key is pressed", () => {
    const onSubmit = mock();
    useKeypadHotkeys({ onSubmit });

    const enterHotkey = registeredHotkeys.find((h) => h.keys === "enter");
    expect(enterHotkey).toBeDefined();

    enterHotkey?.callback({});
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("disables hotkeys when enabled is false", () => {
    const onDigit = mock();
    const onSubmit = mock();
    useKeypadHotkeys({ onDigit, onSubmit, enabled: false });

    registeredHotkeys.forEach((hotkey) => {
      expect(hotkey.options.enabled).toBe(false);
    });
  });

  it("disables hotkeys whose handler is not provided", () => {
    useKeypadHotkeys({ onSubmit: mock() });

    const digitHotkey = registeredHotkeys.find(
      (h) => Array.isArray(h.keys) && h.keys.includes("0")
    );
    expect(digitHotkey?.options.enabled).toBeFalsy();

    const enterHotkey = registeredHotkeys.find((h) => h.keys === "enter");
    expect(enterHotkey?.options.enabled).toBe(true);
  });
});
