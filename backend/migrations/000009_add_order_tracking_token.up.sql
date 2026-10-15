-- Customers who order through the storefront have no account. They follow their order through a
-- secret link containing a random token; like session tokens, only its SHA-256 hash is stored.
-- NULL for orders entered by merchants (no public link).
ALTER TABLE orders ADD COLUMN tracking_token_hash bytea;

ALTER TABLE orders
    ADD CONSTRAINT orders_tracking_token_hash_len CHECK (tracking_token_hash IS NULL OR length(tracking_token_hash) = 32),
    -- Unique per shop; the lookup is always "WHERE tenant_id = ? AND tracking_token_hash = ?",
    -- which this constraint's index serves. (UNIQUE allows many NULLs.)
    ADD CONSTRAINT orders_tenant_tracking_token_key UNIQUE (tenant_id, tracking_token_hash);
