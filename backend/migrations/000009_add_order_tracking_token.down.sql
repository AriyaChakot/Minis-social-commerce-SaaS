ALTER TABLE orders
    DROP CONSTRAINT IF EXISTS orders_tenant_tracking_token_key,
    DROP CONSTRAINT IF EXISTS orders_tracking_token_hash_len,
    DROP COLUMN IF EXISTS tracking_token_hash;
