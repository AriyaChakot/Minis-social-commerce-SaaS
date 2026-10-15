-- Products sold by a shop. The first business table with tenant_id + RLS; later tables follow this pattern.
CREATE TABLE products (
    id           uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    sku          text,
    name         text        NOT NULL,
    description  text        NOT NULL DEFAULT '',
    -- Money as an integer number of satang (1 THB = 100 satang). Never use float for money:
    -- 0.1 + 0.2 = 0.30000000000000004 in floating point.
    price_satang bigint      NOT NULL,
    stock        integer     NOT NULL DEFAULT 0,
    -- Products are archived, never deleted: past orders must keep pointing at them.
    status       text        NOT NULL DEFAULT 'active',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT products_name_not_blank CHECK (length(trim(name)) > 0 AND length(name) <= 200),
    CONSTRAINT products_sku_format CHECK (sku IS NULL OR (length(trim(sku)) > 0 AND length(sku) <= 64)),
    CONSTRAINT products_price_non_negative CHECK (price_satang >= 0),
    CONSTRAINT products_stock_non_negative CHECK (stock >= 0),
    CONSTRAINT products_status_valid CHECK (status IN ('active', 'archived')),

    -- SKU is unique inside a shop, but two shops may use the same SKU.
    -- (UNIQUE allows many NULLs, so products without a SKU never clash.)
    CONSTRAINT products_tenant_sku_key UNIQUE (tenant_id, sku),
    -- Does two jobs:
    --  1. lets order_items use a composite FK (tenant_id, product_id), so an order can never
    --     reference another shop's product;
    --  2. its index serves the main listing query: WHERE tenant_id = ? ORDER BY id DESC.
    CONSTRAINT products_tenant_id_id_key UNIQUE (tenant_id, id)
);

CREATE TRIGGER products_set_updated_at
    BEFORE UPDATE ON products
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Same isolation rule as tenant_members (see 000005). Table grants for mscs_app come from
-- the ALTER DEFAULT PRIVILEGES in 000005, so nothing to grant here.
ALTER TABLE products ENABLE ROW LEVEL SECURITY;
ALTER TABLE products FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON products
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
