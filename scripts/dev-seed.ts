/**
 * Development seed. Creates the first Manager, sample Tables, and sample
 * catalog entities so the Cashier Terminal (POS-a) can be tested end-to-end.
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

async function apiRequest<T>(
  path: string,
  options: { method?: string; body?: unknown; token?: string } = {},
): Promise<T | null> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (options.token) {
    headers["Authorization"] = `Bearer ${options.token}`;
  }

  let body = options.body;
  if (options.method !== "GET" && body !== undefined && typeof body === "object" && body !== null) {
    if (!("request_id" in (body as Record<string, unknown>))) {
      body = { request_id: crypto.randomUUID(), ...(body as object) };
    }
  }

  const res = await fetch(`${BASE}${path}`, {
    method: options.method ?? "POST",
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  const payload = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(`${path} -> ${res.status} ${JSON.stringify(payload)}`);
  }
  return payload as T;
}

interface AuthResponse {
  data?: {
    token?: string;
  };
}

interface IdResponse {
  data?: {
    id?: string;
  };
}

interface MenuResponse {
  data?: {
    categories?: Array<{ id: string; name: string }>;
  };
}

interface OverviewResponse {
  data?: Array<{ name?: string }>;
}

const SEED_TABLES = Array.from({ length: 8 }, (_, i) => `Bàn ${i + 1}`);

/** Creates each sample Table that does not exist yet, so re-running is harmless. */
async function seedTables(token: string): Promise<void> {
  const overview = await apiRequest<OverviewResponse>("/tables/overview", { method: "GET", token });
  const existing = new Set((overview?.data ?? []).map((row) => row.name));
  const missing = SEED_TABLES.filter((name) => !existing.has(name));
  for (const name of missing) {
    await apiRequest("/tables", { token, body: { name } });
  }
  console.log(missing.length > 0 ? `Created tables: ${missing.join(", ")}` : "Tables already seeded.");
}

async function main(): Promise<void> {
  console.log("=== Seeding Development Data ===");

  // 1. Bootstrap or Sign In Manager
  let token = "";
  try {
    await apiRequest("/auth/bootstrap", { body: MANAGER });
    console.log(`Bootstrapped manager ${MANAGER.login_code} with PIN ${MANAGER.pin}`);
  } catch (error) {
    console.log(`Bootstrap skipped: ${String(error)}`);
  }

  try {
    const signInRes = await apiRequest<AuthResponse>("/auth/sign-in", {
      body: { login_code: MANAGER.login_code, pin: MANAGER.pin },
    });
    token = signInRes?.data?.token ?? "";
    console.log("Manager authenticated successfully.");
  } catch (error) {
    console.error("Failed to sign in manager. Ensure Go server is running:", error);
    return;
  }

  // Tables are seeded independently: the catalog check below returns early.
  try {
    await seedTables(token);
  } catch (error) {
    console.error("Failed to seed tables:", error);
  }

  // 2. Check if Catalog is already seeded
  try {
    const menuRes = await apiRequest<MenuResponse>("/catalog/menu/sellable", {
      method: "GET",
      token,
    });
    const categories = menuRes?.data?.categories ?? [];
    if (categories.length > 0) {
      console.log(`Catalog already contains ${categories.length} categories. Seeding completed.`);
      return;
    }
  } catch {
    // Continue seeding if menu read fails or empty
  }

  console.log("Seeding sample catalog entities...");

  // 3. Create Categories
  const catCoffeeRes = await apiRequest<IdResponse>("/catalog/categories", {
    token,
    body: { name: "Cà phê" },
  });
  const catTeaRes = await apiRequest<IdResponse>("/catalog/categories", {
    token,
    body: { name: "Trà trái cây" },
  });
  const catPastryRes = await apiRequest<IdResponse>("/catalog/categories", {
    token,
    body: { name: "Bánh ngọt" },
  });

  const catCoffeeId = catCoffeeRes?.data?.id ?? "";
  const catTeaId = catTeaRes?.data?.id ?? "";
  const catPastryId = catPastryRes?.data?.id ?? "";

  // 4. Create Modifier Groups
  const sugarGroupRes = await apiRequest<IdResponse>("/catalog/modifier-groups", {
    token,
    body: {
      name: "Mức đường",
      min_selections: 1,
      max_selections: 1,
      manager_pin: MANAGER.pin,
      options: [
        { name: "100% đường", surcharge_vnd: 0 },
        { name: "70% đường", surcharge_vnd: 0 },
        { name: "50% đường", surcharge_vnd: 0 },
        { name: "Không đường", surcharge_vnd: 0 },
      ],
      default_option_names: ["100% đường"],
    },
  });

  const toppingGroupRes = await apiRequest<IdResponse>("/catalog/modifier-groups", {
    token,
    body: {
      name: "Topping thêm",
      min_selections: 0,
      max_selections: 3,
      manager_pin: MANAGER.pin,
      options: [
        { name: "Trân châu trắng", surcharge_vnd: 5000 },
        { name: "Thạch nha đam", surcharge_vnd: 5000 },
        { name: "Kem phô mai", surcharge_vnd: 10000 },
      ],
    },
  });

  const sugarGroupId = sugarGroupRes?.data?.id ?? "";
  const toppingGroupId = toppingGroupRes?.data?.id ?? "";

  // Link Modifier Groups to Categories
  if (catCoffeeId && sugarGroupId) {
    await apiRequest(`/catalog/categories/${catCoffeeId}/modifier-groups/${sugarGroupId}`, {
      token,
      body: {},
    });
  }
  if (catCoffeeId && toppingGroupId) {
    await apiRequest(`/catalog/categories/${catCoffeeId}/modifier-groups/${toppingGroupId}`, {
      token,
      body: {},
    });
  }
  if (catTeaId && sugarGroupId) {
    await apiRequest(`/catalog/categories/${catTeaId}/modifier-groups/${sugarGroupId}`, {
      token,
      body: {},
    });
  }
  if (catTeaId && toppingGroupId) {
    await apiRequest(`/catalog/categories/${catTeaId}/modifier-groups/${toppingGroupId}`, {
      token,
      body: {},
    });
  }

  // 5. Create Items
  // Simple Item (no sizes, no modifiers)
  const croissantRes = await apiRequest<IdResponse>("/catalog/items", {
    token,
    body: {
      category_id: catPastryId,
      name: "Croissant bơ tỏi",
      price_vnd: 35000,
      manager_pin: MANAGER.pin,
    },
  });

  // Items with Sizes
  const denRes = await apiRequest<IdResponse>("/catalog/items", {
    token,
    body: {
      category_id: catCoffeeId,
      name: "Cà phê đen",
      manager_pin: MANAGER.pin,
      sizes: [
        { name: "Size S", price_vnd: 25000 },
        { name: "Size M", price_vnd: 29000 },
        { name: "Size L", price_vnd: 35000 },
      ],
    },
  });

  const suaDaRes = await apiRequest<IdResponse>("/catalog/items", {
    token,
    body: {
      category_id: catCoffeeId,
      name: "Cà phê sữa đá",
      manager_pin: MANAGER.pin,
      sizes: [
        { name: "Size S", price_vnd: 29000 },
        { name: "Size M", price_vnd: 35000 },
        { name: "Size L", price_vnd: 42000 },
      ],
    },
  });

  const bacXiuRes = await apiRequest<IdResponse>("/catalog/items", {
    token,
    body: {
      category_id: catCoffeeId,
      name: "Bạc xỉu",
      manager_pin: MANAGER.pin,
      sizes: [
        { name: "Size S", price_vnd: 32000 },
        { name: "Size M", price_vnd: 39000 },
      ],
    },
  });

  const traDaoRes = await apiRequest<IdResponse>("/catalog/items", {
    token,
    body: {
      category_id: catTeaId,
      name: "Trà đào cam sả",
      manager_pin: MANAGER.pin,
      sizes: [
        { name: "Size M", price_vnd: 45000 },
        { name: "Size L", price_vnd: 52000 },
      ],
    },
  });

  // 6. Display fields (BA-1). No images: the seed must run offline.
  const categoryDetails: Array<[string, string, number]> = [
    [catCoffeeId, "coffee", 1],
    [catTeaId, "cup-soda", 2],
    [catPastryId, "croissant", 3],
  ];
  for (const [id, icon, order] of categoryDetails) {
    if (!id) continue;
    await apiRequest(`/catalog/categories/${id}/details`, {
      token,
      method: "PATCH",
      body: { icon, display_order: order },
    });
  }

  const itemDetails: Array<[string | undefined, string, string | null]> = [
    [croissantRes?.data?.id, "CBT", null],
    [denRes?.data?.id, "CFD", null],
    [suaDaRes?.data?.id, "CFSD", "BEST_SELLER"],
    [bacXiuRes?.data?.id, "BX", "SIGNATURE"],
    [traDaoRes?.data?.id, "TDCS", "HOT"],
  ];
  for (const [id, code, badge] of itemDetails) {
    if (!id) continue;
    await apiRequest(`/catalog/items/${id}/details`, {
      token,
      method: "PATCH",
      body: { code, badge, description: null },
    });
  }

  console.log("Sample catalog successfully seeded.");
  console.log("Ready for POS-a UAT testing at http://localhost:5173/");
}

await main();
