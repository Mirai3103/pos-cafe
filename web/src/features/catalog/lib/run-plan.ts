// web/src/features/catalog/lib/run-plan.ts
import { messageForError } from "@/lib/error-messages";
import { NEW_ID, NEW_OPTION_PREFIX, type Command, type Step } from "./save-plan";

export type StepStatus = "done" | "failed" | "pending";

export interface StepResult {
  label: string;
  type: Command["type"];
  status: StepStatus;
  error?: string;
}

/** Ids returned by create steps, used to resolve NEW_ID and new-option refs. */
export interface Created {
  itemId?: string;
  categoryId?: string;
  optionIds: Record<string, string>;
}

export type ExecResult =
  | { created: "item" | "category"; id: string }
  | { created: "option"; id: string; name: string }
  | void;

export type Executor = (cmd: Command, pin: string | null) => Promise<ExecResult>;

export interface RunOutcome {
  ok: boolean;
  results: StepResult[];
  created: Created;
}

function need(id: string | undefined): string {
  if (!id) throw new Error("A step referenced an id its create step did not return");
  return id;
}

export function resolveCommand(cmd: Command, created: Created): Command {
  let out: Command = cmd;
  if ("itemId" in out && out.itemId === NEW_ID) out = { ...out, itemId: need(created.itemId) } as Command;
  if ("categoryId" in out && out.categoryId === NEW_ID) out = { ...out, categoryId: need(created.categoryId) } as Command;
  if (out.type === "group.rule") {
    out = {
      ...out,
      defaultOptionIds: out.defaultOptionIds.map((id) =>
        id.startsWith(NEW_OPTION_PREFIX) ? need(created.optionIds[id.slice(NEW_OPTION_PREFIX.length)]) : id,
      ),
    };
  }
  return out;
}

/** Runs steps in order and stops at the first failure; later steps stay pending. */
export async function runPlan(steps: readonly Step[], exec: Executor, pin: string | null): Promise<RunOutcome> {
  const created: Created = { optionIds: {} };
  const results: StepResult[] = steps.map((s) => ({ label: s.label, type: s.cmd.type, status: "pending" }));
  for (const [i, step] of steps.entries()) {
    try {
      const res = await exec(resolveCommand(step.cmd, created), pin);
      if (res?.created === "item") created.itemId = res.id;
      else if (res?.created === "category") created.categoryId = res.id;
      else if (res?.created === "option") created.optionIds[res.name] = res.id;
      results[i] = { ...results[i], status: "done" };
    } catch (err) {
      results[i] = { ...results[i], status: "failed", error: messageForError(err) };
      return { ok: false, results, created };
    }
  }
  return { ok: true, results, created };
}
