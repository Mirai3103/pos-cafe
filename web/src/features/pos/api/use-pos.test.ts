import { describe, expect, it } from "bun:test";
import {
  useSellableMenu,
  useServiceSession,
  useStartTakeawaySession,
  useAddDraftItem,
  useUpdateDraftItemQuantity,
  useUpdateDraftItemSize,
  useUpdateDraftItemModifiers,
  useUpdateDraftItemPreparationNote,
  useRemoveDraftItem,
  useStartNextDraft,
} from "./use-pos";

describe("use-pos API Seam exports", () => {
  it("exports all required hook functions", () => {
    expect(typeof useSellableMenu).toBe("function");
    expect(typeof useServiceSession).toBe("function");
    expect(typeof useStartTakeawaySession).toBe("function");
    expect(typeof useAddDraftItem).toBe("function");
    expect(typeof useUpdateDraftItemQuantity).toBe("function");
    expect(typeof useUpdateDraftItemSize).toBe("function");
    expect(typeof useUpdateDraftItemModifiers).toBe("function");
    expect(typeof useUpdateDraftItemPreparationNote).toBe("function");
    expect(typeof useRemoveDraftItem).toBe("function");
  });
});

describe("useStartNextDraft", () => {
  it("is exported", () => {
    expect(typeof useStartNextDraft).toBe("function");
  });
});
