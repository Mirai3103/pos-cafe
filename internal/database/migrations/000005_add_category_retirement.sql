-- 000005: Add retirement support to menu_categories, matching menu_items' shape.

ALTER TABLE menu_categories
    ADD COLUMN retired_at TIMESTAMPTZ,
    ADD COLUMN retirement_reason TEXT,
    ADD COLUMN retirement_note TEXT,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE menu_categories
    ADD CONSTRAINT menu_categories_retirement_consistency_check CHECK (
        (retired_at IS NULL AND retirement_reason IS NULL AND retirement_note IS NULL)
        OR (retired_at IS NOT NULL AND retirement_reason IS NOT NULL)
    ),
    ADD CONSTRAINT menu_categories_retirement_note_limit_check CHECK (
        retirement_note IS NULL OR length(retirement_note) <= 500
    );
