DROP FUNCTION IF EXISTS expired_storefront_orders(timestamptz, integer);
DROP INDEX IF EXISTS orders_unpaid_storefront_idx;
ALTER TABLE orders
    DROP CONSTRAINT IF EXISTS orders_cancel_reason_iff_cancelled,
    DROP CONSTRAINT IF EXISTS orders_cancel_reason_valid,
    DROP COLUMN IF EXISTS cancel_reason;
