-- A tenant is one merchant account (one online shop) on the platform.
-- Every tenant-owned table created later will carry a tenant_id that references this table.
CREATE TABLE tenants (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    slug       text        NOT NULL UNIQUE,
    name       text        NOT NULL,
    status     text        NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- slug is used in URLs (e.g. /s/my-shop): lowercase letters, digits and single hyphens only.
    CONSTRAINT tenants_slug_format CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) <= 63),
    CONSTRAINT tenants_name_not_blank CHECK (length(trim(name)) > 0),
    CONSTRAINT tenants_status_valid CHECK (status IN ('active', 'suspended'))
);
