-- Many-to-many link between users and tenants, with the user's role in that tenant.
CREATE TABLE tenant_members (
    tenant_id  uuid        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text        NOT NULL DEFAULT 'staff',
    created_at timestamptz NOT NULL DEFAULT now(),

    -- A user can join a given tenant only once.
    PRIMARY KEY (tenant_id, user_id),
    CONSTRAINT tenant_members_role_valid CHECK (role IN ('owner', 'admin', 'staff'))
);

-- The primary key index starts with tenant_id, so it already serves "members of this tenant".
-- This index serves the reverse lookup "tenants this user belongs to" (used right after login).
CREATE INDEX tenant_members_user_id_idx ON tenant_members (user_id);
