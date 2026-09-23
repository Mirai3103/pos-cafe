/**
 * UAT scaffolding for Web Slice 5. Advances every unfinished Preparation Unit
 * of one active Service Session to FULFILLED, standing in for the Preparation
 * Queue that Slice 6 delivers. Delete this file when Slice 6 ships.
 *
 * Usage: bun run scripts/uat-advance-units.ts <service_number | session_id>
 * Env:   POS_API_BASE (default http://localhost:8080/api/v1)
 *        POS_UAT_LOGIN (default QL01), POS_UAT_PIN (default 1234)
 */
const BASE = process.env.POS_API_BASE ?? "http://localhost:8080/api/v1";
const LOGIN = process.env.POS_UAT_LOGIN ?? "QL01";
const PIN = process.env.POS_UAT_PIN ?? "1234";

// Advance is one step at a time: QUEUED -> IN_PREPARATION -> READY -> FULFILLED.
const STEPS = ["IN_PREPARATION", "READY", "FULFILLED"] as const;
const TERMINAL = new Set(["FULFILLED", "CANCELLED", "WASTED"]);
const BATCH = 50;

interface Unit {
  id?: string;
  state?: string;
}

interface Session {
  id?: string;
  service_number?: string;
  preparation_units?: Unit[];
}

async function api<T>(path: string, init: { method?: string; body?: unknown; token?: string }) {
  const res = await fetch(`${BASE}${path}`, {
    method: init.method ?? "POST",
    headers: {
      "Content-Type": "application/json",
      ...(init.token ? { Authorization: `Bearer ${init.token}` } : {}),
    },
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
  });
  const payload = (await res.json().catch(() => null)) as { data?: T } | null;
  if (!res.ok) throw new Error(`${path} -> ${res.status} ${JSON.stringify(payload)}`);
  return payload?.data as T;
}

async function main(): Promise<void> {
  const target = process.argv[2];
  if (!target) {
    console.error("Usage: bun run scripts/uat-advance-units.ts <service_number | session_id>");
    process.exit(1);
  }

  const auth = await api<{ token?: string }>("/auth/sign-in", {
    body: { login_code: LOGIN, pin: PIN },
  });
  const token = auth?.token ?? "";

  const sessions = await api<Session[]>("/sales/service-sessions", { method: "GET", token });
  const session = (sessions ?? []).find(
    (s) => s.id === target || s.service_number === target,
  );
  if (!session) {
    console.error(`No active session matches "${target}".`);
    process.exit(1);
  }

  const unitIds = (session.preparation_units ?? [])
    .filter((unit) => unit.id && !TERMINAL.has(unit.state ?? ""))
    .map((unit) => unit.id as string);
  if (unitIds.length === 0) {
    console.log(`Session #${session.service_number}: no unfinished units.`);
    return;
  }

  for (const step of STEPS) {
    for (let i = 0; i < unitIds.length; i += BATCH) {
      const batch = unitIds.slice(i, i + BATCH);
      // Units already past this step fail individually; the batch still succeeds.
      await api("/preparation/units/advance-many", {
        token,
        body: { request_id: crypto.randomUUID(), preparation_unit_ids: batch, target_state: step },
      });
    }
    console.log(`Session #${session.service_number}: advanced ${unitIds.length} unit(s) to ${step}.`);
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
