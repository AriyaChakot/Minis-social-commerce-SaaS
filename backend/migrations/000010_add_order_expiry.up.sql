-- Storefront orders that are never paid would hold their stock forever. A background worker
-- cancels them after a payment window; this migration adds what it needs.

-- 1) Why an order was cancelled, so the merchant and the customer can tell "the shop cancelled
--    it" from "it expired". Set exactly when status = 'cancelled' (the CHECK enforces both ways).
ALTER TABLE orders ADD COLUMN cancel_reason text;

-- Existing cancellations were all done by hand in the merchant app.
UPDATE orders SET cancel_reason = 'merchant' WHERE status = 'cancelled';

ALTER TABLE orders
    ADD CONSTRAINT orders_cancel_reason_valid CHECK (cancel_reason IN ('merchant', 'payment_timeout')),
    ADD CONSTRAINT orders_cancel_reason_iff_cancelled CHECK ((status = 'cancelled') = (cancel_reason IS NOT NULL));

-- 2) The worker's query: "pending storefront orders older than X, oldest first".
--    A partial index holds only the rows that can expire, so it stays tiny however many orders exist.
CREATE INDEX orders_unpaid_storefront_idx ON orders (created_at)
    WHERE status = 'pending' AND channel = 'storefront';

-- 3) Finding expired orders must look across ALL shops, but the worker connects as mscs_app,
--    which RLS limits to one shop per transaction. Instead of giving the worker a role that
--    bypasses RLS entirely, this function is a narrow, read-only door: it runs with the rights
--    of its owner (SECURITY DEFINER; the migration role, which RLS does not apply to) and returns
--    only ids. The worker then cancels each order in that order's own tenant scope, so every
--    write still goes through RLS.
CREATE FUNCTION expired_storefront_orders(cutoff timestamptz, max_rows integer)
    RETURNS TABLE (tenant_id uuid, order_id uuid)
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    -- A SECURITY DEFINER function must pin search_path, or a caller could make it run their own
    -- objects (e.g. a fake "orders" table in another schema) with the owner's rights.
    SET search_path = pg_catalog, public
AS $$
    SELECT o.tenant_id, o.id
      FROM orders o
     WHERE o.status = 'pending'
       AND o.channel = 'storefront'
       AND o.created_at < cutoff
     ORDER BY o.created_at
     LIMIT max_rows
$$;

-- Functions are executable by everyone by default; only the app role needs this one.
REVOKE ALL ON FUNCTION expired_storefront_orders(timestamptz, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION expired_storefront_orders(timestamptz, integer) TO mscs_app;
