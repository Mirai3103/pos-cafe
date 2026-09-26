/**
 * Development seed: resets the dev database and seeds demo staff, tables, and
 * the full demo menu (categories, modifier groups, 76 items with images) so
 * the POS, KDS, and Settings screens can be exercised end-to-end.
 *
 * Never run against production: Phase 11 acceptance forbids production
 * configuration from exposing demo identities or sample data.
 *
 * What it does:
 *  1. Truncates every business table in the dev database (direct SQL, keeps
 *     schema_migrations) and wipes old images under MEDIA_DIR/catalog/.
 *  2. Seeds staff (PIN 1234) and sample tables through the API.
 *  3. Creates categories, modifier groups, and all 76 menu items with codes,
 *     badges, descriptions, and images (resources/seeds/{slug}.jpg) through
 *     the command API, so every mutation is validated and audited like a
 *     real request.
 *
 * Requires the API server to be running (`make run`).
 * Usage: make dev-seed   (= cd web && bun run ../scripts/dev-seed.ts)
 * Env:   POS_API_BASE (default http://localhost:8080/api/v1)
 *        DATABASE_URL (same default as config.Config)
 *        MEDIA_DIR    (default <repo>/data/media)
 */
import { SQL } from "bun";
import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync } from "node:fs";
import path from "node:path";
import {
  PIN,
  seedCategories,
  seedItems,
  seedModifierGroups,
  seedStaff,
  seedTables,
} from "./seed-data.ts";

const BASE = process.env.POS_API_BASE ?? "http://localhost:8080/api/v1";
const DATABASE_URL =
  process.env.DATABASE_URL ?? "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos?sslmode=disable";

const REPO_ROOT = path.resolve(import.meta.dir, "..");
const RESOURCES_DIR = path.join(REPO_ROOT, "resources", "seeds");
const MEDIA_DIR = process.env.MEDIA_DIR ? path.resolve(process.env.MEDIA_DIR) : path.join(REPO_ROOT, "data", "media");
const MEDIA_CATALOG_DIR = path.join(MEDIA_DIR, "catalog");

const IMAGE_EXTENSIONS = new Set([".jpg", ".jpeg", ".png", ".webp"]);

async function apiRequest<T>(
  path: string,
  options: { method?: string; body?: unknown; token?: string; form?: FormData } = {},
): Promise<T> {
  const headers: Record<string, string> = {};
  if (options.token) {
    headers["Authorization"] = `Bearer ${options.token}`;
  }

  let body: BodyInit | undefined;
  if (options.form) {
    body = options.form;
  } else if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(options.body);
  }

  const res = await fetch(`${BASE}${path}`, { method: options.method ?? "POST", headers, body });
  const payload = (await res.json().catch(() => null)) as T | null;
  if (!res.ok) {
    throw new Error(`${path} -> ${res.status} ${JSON.stringify(payload)}`);
  }
  return payload as T;
}

/** Auto-injects a request_id so every mutation body satisfies the command pipeline. */
function mutationBody(body: Record<string, unknown>): Record<string, unknown> {
  return { request_id: crypto.randomUUID(), ...body };
}

interface IdResponse {
  data?: { id?: string };
}
interface AuthResponse {
  data?: { token?: string };
}

/** Truncates every business table (keeps schema_migrations) and wipes old catalog images. */
async function resetDatabaseAndMedia(): Promise<void> {
  const sql = new SQL(DATABASE_URL);
  try {
    await sql.unsafe(`DO $$
        DECLARE r RECORD;
        BEGIN
            FOR r IN
                SELECT tablename FROM pg_tables
                WHERE schemaname = 'public' AND tablename <> 'schema_migrations'
            LOOP
                EXECUTE 'TRUNCATE TABLE public.' || quote_ident(r.tablename) || ' RESTART IDENTITY CASCADE';
            END LOOP;
        END $$;`);
    console.log("Database truncated (all business tables).");
  } finally {
    await sql.end();
  }

  mkdirSync(MEDIA_CATALOG_DIR, { recursive: true });
  let removed = 0;
  for (const file of readdirSync(MEDIA_CATALOG_DIR)) {
    const ext = path.extname(file).toLowerCase();
    if (IMAGE_EXTENSIONS.has(ext) || file.startsWith(".tmp-")) {
      rmSync(path.join(MEDIA_CATALOG_DIR, file));
      removed++;
    }
  }
  console.log(`Wiped ${removed} old image file(s) under ${MEDIA_CATALOG_DIR}.`);
}

async function seedStaffAccounts(): Promise<string> {
  const first = seedStaff.find((s) => s.bootstrap) ?? seedStaff[0];
  await apiRequest("/auth/bootstrap", { body: { display_name: first.displayName, login_code: first.loginCode, pin: first.pin } });
  const signIn = await apiRequest<AuthResponse>("/auth/sign-in", { body: { login_code: first.loginCode, pin: first.pin } });
  const token = signIn?.data?.token ?? "";
  if (!token) throw new Error("sign-in returned no token");
  console.log(`Bootstrapped ${first.loginCode} and signed in.`);

  for (const staff of seedStaff.filter((s) => !s.bootstrap)) {
    await apiRequest("/staff", {
      token,
      body: mutationBody({
        display_name: staff.displayName,
        login_code: staff.loginCode,
        enabled: true,
        roles: staff.roles,
        pin: staff.pin,
        manager_pin: PIN,
      }),
    });
    console.log(`Created staff ${staff.loginCode} (${staff.roles.join("/")}).`);
  }
  return token;
}

async function seedMenuCategories(token: string): Promise<Map<string, string>> {
  const ids = new Map<string, string>();
  for (const [index, category] of seedCategories.entries()) {
    const res = await apiRequest<IdResponse>("/catalog/categories", { token, body: mutationBody({ name: category.name }) });
    const id = res?.data?.id ?? "";
    if (!id) throw new Error(`category ${category.name}: no id in response`);
    await apiRequest(`/catalog/categories/${id}/details`, {
      token,
      method: "PATCH",
      body: mutationBody({ icon: category.icon, display_order: index + 1 }),
    });
    ids.set(category.name, id);
  }
  console.log(`Seeded ${seedCategories.length} categories with icons and display order.`);
  return ids;
}

async function seedAllModifierGroups(token: string, categoryIds: Map<string, string>): Promise<void> {
  for (const group of seedModifierGroups) {
    const res = await apiRequest<IdResponse>("/catalog/modifier-groups", {
      token,
      body: mutationBody({
        name: group.name,
        min_selections: group.minSelections,
        max_selections: group.maxSelections,
        manager_pin: PIN,
        options: group.options.map((o) => ({ name: o.name, surcharge_vnd: o.surchargeVnd })),
        ...(group.defaultOptionNames ? { default_option_names: group.defaultOptionNames } : {}),
      }),
    });
    const groupId = res?.data?.id ?? "";
    if (!groupId) throw new Error(`modifier group ${group.name}: no id in response`);
    for (const categoryName of group.categories) {
      const categoryId = categoryIds.get(categoryName);
      if (!categoryId) continue;
      await apiRequest(`/catalog/categories/${categoryId}/modifier-groups/${groupId}`, {
        token,
        body: mutationBody({}),
      });
    }
    console.log(`Seeded modifier group "${group.name}" on ${group.categories.length} categories.`);
  }
}

async function uploadImage(token: string, itemId: string, slug: string): Promise<void> {
  const imagePath = path.join(RESOURCES_DIR, `${slug}.jpg`);
  if (!existsSync(imagePath)) {
    throw new Error(`image not found: ${imagePath}`);
  }
  const form = new FormData();
  form.set("request_id", crypto.randomUUID());
  form.set("file", new File([readFileSync(imagePath)], `${slug}.jpg`, { type: "image/jpeg" }));
  await apiRequest(`/catalog/items/${itemId}/image`, { token, method: "PUT", form });
}

async function seedMenuItems(token: string, categoryIds: Map<string, string>): Promise<number> {
  let failures = 0;
  for (const item of seedItems) {
    const categoryId = categoryIds.get(item.category) ?? "";
    try {
      const res = await apiRequest<IdResponse>("/catalog/items", {
        token,
        body: mutationBody({
          category_id: categoryId,
          name: item.name,
          manager_pin: PIN,
          ...(item.sizes
            ? { sizes: item.sizes.map((s) => ({ name: s.name, price_vnd: s.priceVnd })) }
            : { price_vnd: item.priceVnd }),
        }),
      });
      const itemId = res?.data?.id ?? "";
      if (!itemId) throw new Error("no id in response");

      await apiRequest(`/catalog/items/${itemId}/details`, {
        token,
        method: "PATCH",
        body: mutationBody({ code: item.code, badge: item.badge ?? null, description: item.description }),
      });
      await uploadImage(token, itemId, item.slug);
      console.log(`  ✓ ${item.name} (${item.code})`);
    } catch (error) {
      failures++;
      console.error(`  ✗ ${item.name} (${item.code}): ${String(error)}`);
    }
  }
  return failures;
}

async function seedSampleTables(token: string): Promise<void> {
  for (const name of seedTables) {
    await apiRequest("/tables", { token, body: mutationBody({ name }) });
  }
  console.log(`Seeded ${seedTables.length} tables.`);
}

async function main(): Promise<void> {
  console.log("=== Development Seed (reset + full demo data) ===");

  await resetDatabaseAndMedia();

  const token = await seedStaffAccounts();

  await seedSampleTables(token);
  const categoryIds = await seedMenuCategories(token);
  await seedAllModifierGroups(token, categoryIds);
  const failures = await seedMenuItems(token, categoryIds);

  console.log(`=== Done: ${seedStaff.length} staff, ${seedTables.length} tables, ${seedCategories.length} categories, ${seedModifierGroups.length} modifier groups, ${seedItems.length - failures}/${seedItems.length} items with images. ===`);
  console.log("Ready for POS/UAT testing at http://localhost:5173/");
  if (failures > 0) {
    process.exit(1);
  }
}

await main();
