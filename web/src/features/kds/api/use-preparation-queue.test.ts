import { describe, expect, it } from "bun:test";
import { usePreparationQueue, PREPARATION_QUEUE_POLL_MS } from "./use-preparation-queue";
import { ERROR_MESSAGES } from "@/lib/error-messages";

describe("usePreparationQueue", () => {
  it("is exported", () => {
    expect(typeof usePreparationQueue).toBe("function");
  });

  it("polls every 5 seconds, matching the existing IN_PREPARATION convention", () => {
    expect(PREPARATION_QUEUE_POLL_MS).toBe(5_000);
  });
});

describe("preparation error messages", () => {
  it("maps every stable preparation error code to Vietnamese", () => {
    for (const code of [
      "PREPARATION_UNIT_NOT_FOUND",
      "INVALID_TRANSITION",
      "INVALID_STORED_RESULT",
      "PREPARATION_ALERT_NOT_FOUND",
      "PREPARATION_WASTE_NOT_FOUND",
      "INVALID_PREPARATION_REASON",
      "PREPARATION_ALERT_ALREADY_ACKNOWLEDGED",
      "PREPARATION_WASTE_ALREADY_REMADE",
      "NOT_AUTHORIZED",
      "INVALID_INPUT",
    ]) {
      expect(ERROR_MESSAGES[code]).toBeTruthy();
    }
  });
});
