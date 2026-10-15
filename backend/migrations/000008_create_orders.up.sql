-- Orders, their line items, and a per-shop counter for human-friendly order numbers.

-- ---------------------------------------------------------------------------
-- order_counters: one row per shop holding the last order number used.
-- Next number = INSERT ... ON CONFLICT DO UPDATE SET last_number = last_number + 1 RETURNING.
-- That statement row-locks the shop's counter until commit, so two concurrent orders can never
-- get the same number (unlike SELECT max(order_number) + 1, which races).
-- ---------------------------------------------------------------------------
CREATE TABLE order_counters (
    tenant_id   uuid   PRIMARY KEY REFERENCES tenants (id) ON DELETE CASCADE,
    last_number bigint NOT NULL
);

-- ---------------------------------------------------------------------------
-- orders
-- ---------------------------------------------------------------------------
CREATE TABLE orders (
    id                  uuid        PRIMARY KEY DEFAULT uuidv7(),
    -- RESTRICT: a shop that has orders cannot be deleted by accident (financial records).
    tenant_id           uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    order_number        bigint      NOT NULL,
    customer_name       text        NOT NULL,
    customer_phone      text        NOT NULL DEFAULT '',
    shipping_address    text        NOT NULL DEFAULT '',
    channel             text        NOT NULL DEFAULT 'manual',
    note                text        NOT NULL DEFAULT '',
    status              text        NOT NULL DEFAULT 'pending',
    subtotal_satang     bigint      NOT NULL,
    shipping_fee_satang bigint      NOT NULL DEFAULT 0,
    total_satang        bigint      NOT NULL,
    created_by          uuid        REFERENCES users (id) ON DELETE SET NULL,
    paid_at             timestamptz,
    shipped_at          timestamptz,
    completed_at        timestamptz,
    cancelled_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT orders_customer_name_not_blank CHECK (length(trim(customer_name)) > 0),
    CONSTRAINT orders_channel_valid CHECK (channel IN ('manual', 'line', 'facebook', 'instagram', 'storefront')),
    CONSTRAINT orders_status_valid CHECK (status IN ('pending', 'paid', 'shipped', 'completed', 'cancelled')),
    CONSTRAINT orders_amounts_non_negative CHECK (subtotal_satang >= 0 AND shipping_fee_satang >= 0),
    -- The database itself refuses a total that does not add up.
    CONSTRAINT orders_total_matches CHECK (total_satang = subtotal_satang + shipping_fee_satang),

    CONSTRAINT orders_tenant_number_key UNIQUE (tenant_id, order_number),
    -- Target of the composite FK from order_items; its index also serves the listing query.
    CONSTRAINT orders_tenant_id_id_key UNIQUE (tenant_id, id)
);

-- "Orders waiting for payment, newest first" is the screen merchants open most.
CREATE INDEX orders_tenant_status_id_idx ON orders (tenant_id, status, id DESC);

CREATE TRIGGER orders_set_updated_at
    BEFORE UPDATE ON orders
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- order_items: one line per product. Name, SKU and price are COPIED from the product
-- (a snapshot): changing a product's price later must not change past orders.
-- ---------------------------------------------------------------------------
CREATE TABLE order_items (
    id                uuid    PRIMARY KEY DEFAULT uuidv7(),
    tenant_id         uuid    NOT NULL,
    order_id          uuid    NOT NULL,
    product_id        uuid    NOT NULL,
    product_name      text    NOT NULL,
    sku               text,
    unit_price_satang bigint  NOT NULL,
    quantity          integer NOT NULL,
    line_total_satang bigint  NOT NULL,

    CONSTRAINT order_items_quantity_positive CHECK (quantity > 0),
    CONSTRAINT order_items_price_non_negative CHECK (unit_price_satang >= 0),
    CONSTRAINT order_items_line_total_matches CHECK (line_total_satang = unit_price_satang * quantity),
    CONSTRAINT order_items_order_product_key UNIQUE (order_id, product_id),

    -- Composite FKs: the item, its order and its product must all belong to the SAME shop.
    -- A bug that mixes shops is rejected by the database, not just by the code.
    CONSTRAINT order_items_order_fkey FOREIGN KEY (tenant_id, order_id)
        REFERENCES orders (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT order_items_product_fkey FOREIGN KEY (tenant_id, product_id)
        REFERENCES products (tenant_id, id) ON DELETE RESTRICT
);

-- Postgres does not index FK columns automatically. Without this, every product delete/archive
-- check and "which orders contain product X" would scan all order_items.
CREATE INDEX order_items_tenant_product_idx ON order_items (tenant_id, product_id);

-- ---------------------------------------------------------------------------
-- Row-Level Security: same isolation rule as products (see 000005 / 000007).
-- ---------------------------------------------------------------------------
ALTER TABLE order_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_counters FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON order_counters
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE orders FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON orders
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE order_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_items FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON order_items
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
