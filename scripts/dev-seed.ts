/**
 * Development seed. Creates the first Manager so the UI can be signed into.
 *
 * Never run against production: Phase 11 acceptance forbids production
 * configuration from exposing demo identities or sample sales.
 *
 * Usage: bun run scripts/dev-seed.ts
 */
const BASE = process.env.POS_API_BASE ?? "http://localhost:8080/api/v1";

const MANAGER = {
  display_name: "Quan Ly Demo",
  login_code: "QL01",
  pin: "1234",
};

async function post(path: string, body: unknown): Promise<unknown> {
  const res = await fetch(`${BASE}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const payload = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(`${path} -> ${res.status} ${JSON.stringify(payload)}`);
  }
  return payload;
}

async function main(): Promise<void> {
  try {
    await post("/auth/bootstrap", MANAGER);
    console.log(`Bootstrapped manager ${MANAGER.login_code} with PIN ${MANAGER.pin}`);
  } catch (error) {
    console.log(`Bootstrap skipped or failed, continuing: ${String(error)}`);
  }
  console.log("Sign in at http://localhost:5173/auth/login");
}

await main();
