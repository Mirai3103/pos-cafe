-- 1. Staff Identities
CREATE TABLE IF NOT EXISTS staff_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name VARCHAR(120) NOT NULL,
    login_code VARCHAR(24) NOT NULL,
    pin_hash VARCHAR(72) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS staff_login_code_unique 
    ON staff_identities (upper(btrim(login_code)));

-- 2. Staff Operational Roles (Multi-Role Support)
CREATE TABLE IF NOT EXISTS staff_operational_roles (
    staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL CHECK (role IN ('MANAGER', 'CASHIER', 'BARISTA')),
    PRIMARY KEY (staff_identity_id, role)
);

-- 3. Staff Access Sessions
CREATE TABLE IF NOT EXISTS staff_access_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE CASCADE,
    state VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'locked')),
    active_workspace VARCHAR(20) CHECK (active_workspace IN ('cashier', 'manager', 'preparation')),
    last_authenticated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_human_activity_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_staff_access_sessions_lookup 
    ON staff_access_sessions (token_hash) 
    WHERE revoked_at IS NULL;

-- 4. Unified Idempotency Table (ADR-005)
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key UUID NOT NULL,
    actor_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE CASCADE,
    action VARCHAR(50) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    response_code INT NOT NULL,
    response_body JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_id, key)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_created_at 
    ON idempotency_keys (created_at);
