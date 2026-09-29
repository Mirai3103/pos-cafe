// web/src/features/catalog/lib/run-plan.test.ts
import { describe, expect, it } from "bun:test";
import { messageForError } from "@/lib/error-messages";
import { ApiError } from "@/lib/unwrap";
import { resolveCommand, runPlan, type Executor } from "./run-plan";
import { NEW_ID, NEW_OPTION_PREFIX, type Command, type Step } from "./save-plan";

const create: Step = { label: "Tạo món", cmd: { type: "item.create", categoryId: "c", name: "A", priceVnd: 1000 } };
const groups: Step = {
  label: "Cập nhật nhóm topping",
  cmd: { type: "item.groups", itemId: NEW_ID, directGroupIds: ["g"], excludedGroupIds: [] },
};
const details: Step = {
  label: "Cập nhật mã, huy hiệu & mô tả",
  cmd: { type: "item.details", itemId: NEW_ID, code: "a", badge: null, description: null },
};

describe("runPlan", () => {
  it("runs steps in order, resolving NEW_ID from the create step", async () => {
    const seen: Command[] = [];
    const exec: Executor = async (cmd) => {
      seen.push(cmd);
      if (cmd.type === "item.create") return { created: "item", id: "i-1" };
    };
    const out = await runPlan([create, groups], exec, null);
    expect(out.ok).toBe(true);
    expect(out.created.itemId).toBe("i-1");
    expect(seen[1]).toEqual({ type: "item.groups", itemId: "i-1", directGroupIds: ["g"], excludedGroupIds: [] });
    expect(out.results.map((r) => r.status)).toEqual(["done", "done"]);
  });

  it("stops at the first failure and leaves later steps pending", async () => {
    const err = new ApiError(409, "CATALOG_CODE_CONFLICT", "code taken");
    const exec: Executor = async (cmd) => {
      if (cmd.type === "item.create") return { created: "item", id: "i-1" };
      if (cmd.type === "item.details") throw err;
    };
    const out = await runPlan([create, details, groups], exec, null);
    expect(out.ok).toBe(false);
    expect(out.results.map((r) => r.status)).toEqual(["done", "failed", "pending"]);
    expect(out.results[1].error).toBe(messageForError(err));
    expect(out.results[1].type).toBe("item.details");
    expect(out.created.itemId).toBe("i-1");
  });

  it("passes the PIN to every step", async () => {
    const pins: (string | null)[] = [];
    const exec: Executor = async (cmd, pin) => {
      pins.push(pin);
      if (cmd.type === "item.create") return { created: "item", id: "i-1" };
    };
    await runPlan([create, groups], exec, "1234");
    expect(pins).toEqual(["1234", "1234"]);
  });
});

describe("resolveCommand", () => {
  it("resolves options created earlier in the plan", () => {
    const rule: Command = {
      type: "group.rule",
      groupId: "g",
      min: 0,
      max: 2,
      defaultOptionIds: ["o-1", `${NEW_OPTION_PREFIX}Nha đam`],
    };
    expect(resolveCommand(rule, { optionIds: { "Nha đam": "o-2" } })).toEqual({ ...rule, defaultOptionIds: ["o-1", "o-2"] });
  });

  it("resolves a new category id and leaves real ids alone", () => {
    const cmd: Command = { type: "category.groups", categoryId: NEW_ID, groupIds: [] };
    expect(resolveCommand(cmd, { categoryId: "c-9", optionIds: {} })).toEqual({ ...cmd, categoryId: "c-9" });
    const move: Command = { type: "item.move", itemId: "i", categoryId: "c-1" };
    expect(resolveCommand(move, { optionIds: {} })).toEqual(move);
  });
});
