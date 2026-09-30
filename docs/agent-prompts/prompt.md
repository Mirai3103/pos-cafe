Implement Phase 2 Catalog using Subagent-Driven Development.

     Repository:
     `/home/laffy/Desktop/go-vertical-slice-template-main/pos-cafe`

     Authoritative documents:
     - Spec: `docs/superpowers/specs/2026-09-10-catalog-slice-design.md`
     - Plan: `docs/superpowers/plans/2026-09-10-catalog-slice.md`
     - TypeScript reference, read-only: `/home/laffy/cafe-pos`

     The spec is approved and is the binding authority. Do not brainstorm or rewrite the design/plan.

     Before any action:
     1. Invoke `using-git-worktrees` and create or verify an isolated worktree. Do not implement on
     main/master.
     2. Invoke `subagent-driven-development`.
     3. Note that the spec and plan may currently be untracked in the original worktree. Preserve them
     and ensure both are available in the isolated worktree; do not discard or overwrite them.
     4. Initialize the plan-specific SDD workspace and progress ledger.
     5. Read the spec and plan once, create one todo per task, and perform the required preflight task/
     interface conflict table.

     Execute all 14 tasks continuously:
     - Dispatch one fresh implementer subagent at a time.
     - Use generated task briefs as the sole task requirements.
     - Require TDD and fresh test evidence.
     - Allow the task commits specified by the plan.
     - Generate a review package after each task.
     - Dispatch an independent task reviewer for both spec compliance and code quality.
     - Run the prescribed fix/re-review loop for Critical or Important findings.
     - Record progress, commits, findings, and rulings in the ledger.
     - Do not pause between tasks or ask whether to continue.
     - Resolve ordinary ambiguities using the approved spec and record each ruling.
     - Stop only for irreversible/destructive actions, security-sensitive actions, external side
     effects such as push/merge/publish, or a plan defect for which every path is a guess.

     Important constraints:
     - Preserve canonical TypeScript business behavior in idiomatic Go.
     - Canonical domain documentation wins over known TypeScript defects.
     - The existing Go `internal/category` code is disposable boilerplate; no compatibility is required.
     - Keep `/home/laffy/cafe-pos` read-only.
     - Do not push, merge, publish, or modify unrelated user changes.
     - Use CodeGraph before grep/read because both repositories are indexed.
     - Use `apply_patch` for manual file edits.
     - Run integration packages with `-p 1`.

     After all tasks:
     1. Run the plan’s complete verification commands.
     2. Dispatch the broad final reviewer using the full branch review package and the most capable
     available model.
     3. Run one final fix wave and scoped re-review if needed.
     4. Report all ledger rulings and verification evidence.
     5. Invoke `finishing-a-development-branch` and present integration options without merging or
     pushing automatically.
