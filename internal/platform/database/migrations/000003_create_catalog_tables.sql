-- 000003: Create Catalog tables (Phase 2)
-- Replaces disposable `categories` table with authoritative Catalog schema.

-- 1. Menu Categories
CREATE TABLE menu_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 2. Menu Items
CREATE TABLE menu_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id UUID NOT NULL REFERENCES menu_categories(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    price_vnd BIGINT,
    available BOOLEAN NOT NULL DEFAULT true,
    retired_at TIMESTAMPTZ,
    retirement_reason TEXT,
    retirement_note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT menu_items_price_check CHECK (
        price_vnd IS NULL OR price_vnd BETWEEN 1 AND 2147483647
    ),
    CONSTRAINT menu_items_retirement_consistency_check CHECK (
        (retired_at IS NULL AND retirement_reason IS NULL AND retirement_note IS NULL)
        OR (retired_at IS NOT NULL AND retirement_reason IS NOT NULL)
    ),
    CONSTRAINT menu_items_retirement_note_limit_check CHECK (
        retirement_note IS NULL OR length(retirement_note) <= 500
    ),
    UNIQUE (category_id, normalized_name)
);

-- 3. Menu Item Sizes
CREATE TABLE menu_item_sizes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    price_vnd BIGINT NOT NULL,
    available BOOLEAN NOT NULL DEFAULT true,
    retired_at TIMESTAMPTZ,
    retirement_reason TEXT,
    retirement_note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT menu_item_sizes_price_check CHECK (
        price_vnd BETWEEN 1 AND 2147483647
    ),
    CONSTRAINT menu_item_sizes_retirement_consistency_check CHECK (
        (retired_at IS NULL AND retirement_reason IS NULL AND retirement_note IS NULL)
        OR (retired_at IS NOT NULL AND retirement_reason IS NOT NULL)
    ),
    CONSTRAINT menu_item_sizes_retirement_note_limit_check CHECK (
        retirement_note IS NULL OR length(retirement_note) <= 500
    ),
    UNIQUE (menu_item_id, normalized_name)
);

-- 4. Modifier Groups
CREATE TABLE modifier_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL UNIQUE,
    min_selections INT NOT NULL DEFAULT 0,
    max_selections INT NOT NULL DEFAULT 1,
    retired_at TIMESTAMPTZ,
    retirement_reason TEXT,
    retirement_note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT modifier_groups_bounds_check CHECK (
        min_selections >= 0 AND max_selections >= 1
        AND min_selections <= max_selections
    ),
    CONSTRAINT modifier_groups_retirement_consistency_check CHECK (
        (retired_at IS NULL AND retirement_reason IS NULL AND retirement_note IS NULL)
        OR (retired_at IS NOT NULL AND retirement_reason IS NOT NULL)
    ),
    CONSTRAINT modifier_groups_retirement_note_limit_check CHECK (
        retirement_note IS NULL OR length(retirement_note) <= 500
    )
);

-- 5. Modifier Options
CREATE TABLE modifier_options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    modifier_group_id UUID NOT NULL REFERENCES modifier_groups(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    surcharge_vnd BIGINT NOT NULL DEFAULT 0,
    available BOOLEAN NOT NULL DEFAULT true,
    retired_at TIMESTAMPTZ,
    retirement_reason TEXT,
    retirement_note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT modifier_options_surcharge_check CHECK (
        surcharge_vnd BETWEEN 0 AND 2147483647
    ),
    CONSTRAINT modifier_options_retirement_consistency_check CHECK (
        (retired_at IS NULL AND retirement_reason IS NULL AND retirement_note IS NULL)
        OR (retired_at IS NOT NULL AND retirement_reason IS NOT NULL)
    ),
    CONSTRAINT modifier_options_retirement_note_limit_check CHECK (
        retirement_note IS NULL OR length(retirement_note) <= 500
    ),
    UNIQUE (modifier_group_id, normalized_name)
);

-- 6. Association Tables

-- Direct item-to-modifier-group assignments
CREATE TABLE item_modifier_groups (
    menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    modifier_group_id UUID NOT NULL REFERENCES modifier_groups(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (menu_item_id, modifier_group_id)
);

-- Category-level modifier group defaults (inherited by items)
CREATE TABLE category_modifier_groups (
    menu_category_id UUID NOT NULL REFERENCES menu_categories(id) ON DELETE RESTRICT,
    modifier_group_id UUID NOT NULL REFERENCES modifier_groups(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (menu_category_id, modifier_group_id)
);

-- Item exclusions of inherited category modifier groups
CREATE TABLE item_modifier_group_exclusions (
    menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    modifier_group_id UUID NOT NULL REFERENCES modifier_groups(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (menu_item_id, modifier_group_id)
);

-- Explicit default options for a modifier group
CREATE TABLE modifier_group_default_options (
    modifier_group_id UUID NOT NULL REFERENCES modifier_groups(id) ON DELETE RESTRICT,
    modifier_option_id UUID NOT NULL REFERENCES modifier_options(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (modifier_group_id, modifier_option_id)
);

-- 7. Catalog Mutation Requests (Idempotency)
CREATE TABLE catalog_mutation_requests (
    actor_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    request_id UUID NOT NULL,
    operation TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_code INT NOT NULL,
    response_body JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_id, request_id)
);

-- 8. Audit Events
CREATE TABLE audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type TEXT NOT NULL,
    actor_id UUID REFERENCES staff_identities(id) ON DELETE RESTRICT,
    session_id UUID REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    details JSONB NOT NULL DEFAULT '{}',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_events_occurred_at ON audit_events (occurred_at DESC);
