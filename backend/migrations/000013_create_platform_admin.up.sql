-- The platform console: the people who run MSCS (not shop staff) can see every shop and
-- suspend one that breaks the rules.

-- Who is a platform admin. The API may only READ this table: making someone an admin needs the
-- database owner (a person with server access), never an API call, so no bug or stolen merchant
-- session can grant itself platform rights.
CREATE TABLE platform_admins (
    user_id    uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON platform_admins FROM mscs_app;

-- Why and since when a shop is suspended (status already allows 'suspended', migration 000001).
ALTER TABLE tenants
    ADD COLUMN suspended_at     timestamptz,
    ADD COLUMN suspended_reason text NOT NULL DEFAULT '',
    ADD CONSTRAINT tenants_suspension_consistent
        CHECK ((status = 'suspended') = (suspended_at IS NOT NULL));

-- Every platform action, append-only: the API can add rows but not change or delete them, so the
-- record of who suspended which shop cannot be rewritten afterwards.
CREATE TABLE platform_audit_log (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    actor_id   uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    action     text        NOT NULL CHECK (action IN ('tenant.suspend', 'tenant.reactivate')),
    tenant_id  uuid        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    reason     text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX platform_audit_log_tenant_idx ON platform_audit_log (tenant_id);
REVOKE UPDATE, DELETE, TRUNCATE ON platform_audit_log FROM mscs_app;

-- Products and orders have Row-Level Security: the API only ever sees one shop at a time.
-- The console needs totals across shops, so these functions run as their owner (SECURITY
-- DEFINER, which bypasses RLS) and return counts and sums, never a customer or an order (the
-- only personal data is the shop owner's email, so the platform can contact the shop).
-- search_path is fixed so a malicious schema cannot swap the tables they read.
CREATE FUNCTION platform_tenant_stats()
RETURNS TABLE (
    tenant_id       uuid,
    owner_email     text,
    member_count    bigint,
    product_count   bigint,
    order_count     bigint,
    revenue_satang  bigint,
    last_order_at   timestamptz
)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT t.id,
           -- Whom to contact about the shop (the first owner, if several).
           (SELECT u.email FROM tenant_members m JOIN users u ON u.id = m.user_id
             WHERE m.tenant_id = t.id AND m.role = 'owner' ORDER BY u.created_at LIMIT 1),
           (SELECT count(*) FROM tenant_members m WHERE m.tenant_id = t.id),
           (SELECT count(*) FROM products p WHERE p.tenant_id = t.id AND p.status = 'active'),
           (SELECT count(*) FROM orders o WHERE o.tenant_id = t.id),
           -- Revenue = orders the customer has paid for (cancelled and unpaid ones excluded).
           (SELECT coalesce(sum(o.total_satang), 0)::bigint FROM orders o
             WHERE o.tenant_id = t.id AND o.status IN ('paid', 'shipped', 'completed')),
           (SELECT max(o.created_at) FROM orders o WHERE o.tenant_id = t.id)
    FROM tenants t
$$;

-- Orders per day (Thai calendar days) across all shops, from `since`.
CREATE FUNCTION platform_daily_orders(since timestamptz)
RETURNS TABLE (day date, order_count bigint, revenue_satang bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT (o.created_at AT TIME ZONE 'Asia/Bangkok')::date AS day,
           count(*),
           coalesce(sum(o.total_satang) FILTER (WHERE o.status IN ('paid', 'shipped', 'completed')), 0)::bigint
    FROM orders o
    WHERE o.created_at >= since
    GROUP BY 1
    ORDER BY 1
$$;

-- Functions are callable by everyone by default; only the API role needs these.
REVOKE EXECUTE ON FUNCTION platform_tenant_stats(), platform_daily_orders(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform_tenant_stats(), platform_daily_orders(timestamptz) TO mscs_app;
