-- Postgres has no "ON UPDATE CURRENT_TIMESTAMP" like MySQL.
-- This shared trigger function sets updated_at on every UPDATE; attach it to each table that has the column.
CREATE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER tenants_set_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
