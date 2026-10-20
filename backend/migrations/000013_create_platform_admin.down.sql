DROP FUNCTION platform_daily_orders(timestamptz);
DROP FUNCTION platform_tenant_stats();
DROP TABLE platform_audit_log;

-- Suspended shops become active again: without suspended_at the old CHECK still allows the
-- status, but nothing would explain it.
UPDATE tenants SET status = 'active' WHERE status = 'suspended';
ALTER TABLE tenants
    DROP CONSTRAINT tenants_suspension_consistent,
    DROP COLUMN suspended_reason,
    DROP COLUMN suspended_at;

DROP TABLE platform_admins;
