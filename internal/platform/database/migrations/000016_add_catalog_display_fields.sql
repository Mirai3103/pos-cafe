-- 000016: Menu display fields (BA-1, ADR-058).
--
-- Code, badge, description, image, icon, and display order change how the
-- menu looks, never what is sold or what it costs. They are edited in place
-- and never copied into Committed Item snapshots. Every statement is
-- rerunnable.

ALTER TABLE menu_items
    ADD COLUMN IF NOT EXISTS code TEXT,
    ADD COLUMN IF NOT EXISTS normalized_code TEXT,
    ADD COLUMN IF NOT EXISTS badge TEXT,
    ADD COLUMN IF NOT EXISTS description TEXT,
    ADD COLUMN IF NOT EXISTS image_key TEXT;

-- normalized_code IS NOT NULL is spelled out: against a NULL normalized_code
-- the regex yields NULL, and a CHECK passes on NULL, which would let a code
-- exist without its normalized form.
ALTER TABLE menu_items DROP CONSTRAINT IF EXISTS menu_items_code_check;
ALTER TABLE menu_items ADD CONSTRAINT menu_items_code_check CHECK (
    (code IS NULL AND normalized_code IS NULL)
    OR (code IS NOT NULL AND normalized_code IS NOT NULL
        AND normalized_code ~ '^[a-z0-9]{1,12}$')
);

ALTER TABLE menu_items DROP CONSTRAINT IF EXISTS menu_items_badge_check;
ALTER TABLE menu_items ADD CONSTRAINT menu_items_badge_check CHECK (
    badge IS NULL OR badge IN ('BEST_SELLER', 'HOT', 'NEW', 'SIGNATURE', 'CHEF_PICK')
);

ALTER TABLE menu_items DROP CONSTRAINT IF EXISTS menu_items_description_check;
ALTER TABLE menu_items ADD CONSTRAINT menu_items_description_check CHECK (
    description IS NULL OR char_length(description) BETWEEN 1 AND 300
);

ALTER TABLE menu_items DROP CONSTRAINT IF EXISTS menu_items_image_key_check;
ALTER TABLE menu_items ADD CONSTRAINT menu_items_image_key_check CHECK (
    image_key IS NULL OR image_key ~ '^[0-9a-f]{64}\.(jpg|png|webp)$'
);

-- A retired item's code may be reused.
CREATE UNIQUE INDEX IF NOT EXISTS menu_items_active_code_key
    ON menu_items (normalized_code)
    WHERE retired_at IS NULL AND normalized_code IS NOT NULL;

ALTER TABLE menu_categories
    ADD COLUMN IF NOT EXISTS icon TEXT,
    ADD COLUMN IF NOT EXISTS display_order INT NOT NULL DEFAULT 0;

ALTER TABLE menu_categories DROP CONSTRAINT IF EXISTS menu_categories_icon_check;
ALTER TABLE menu_categories ADD CONSTRAINT menu_categories_icon_check CHECK (
    icon IS NULL OR icon ~ '^[a-z0-9-]{1,40}$'
);

ALTER TABLE menu_categories DROP CONSTRAINT IF EXISTS menu_categories_display_order_check;
ALTER TABLE menu_categories ADD CONSTRAINT menu_categories_display_order_check CHECK (
    display_order BETWEEN 0 AND 9999
);

-- Existing categories keep their creation order.
UPDATE menu_categories c
SET display_order = o.rn
FROM (
    SELECT id, row_number() OVER (ORDER BY created_at, id) AS rn
    FROM menu_categories
) o
WHERE c.id = o.id AND c.display_order = 0;
