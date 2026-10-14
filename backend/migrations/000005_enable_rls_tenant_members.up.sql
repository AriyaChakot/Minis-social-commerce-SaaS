-- Requires the login role mscs_app (created by docker/postgres/initdb/01-create-app-role.sh,
-- or by an operator on a managed database) BEFORE this migration runs.

-- 1) Privileges: the API may read and write rows, but never change the schema.
GRANT USAGE ON SCHEMA public TO mscs_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON tenants, users, tenant_members TO mscs_app;
-- Tables created by later migrations (run as this same owner role) get the same grants automatically.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO mscs_app;

-- 2) Row-Level Security on tenant_members.
-- The API tells Postgres who is asking, per transaction:
--   SELECT set_config('app.tenant_id', '<uuid>', true);   -- the shop being worked on
--   SELECT set_config('app.user_id',   '<uuid>', true);   -- the logged-in user
-- current_setting(..., true) returns NULL when unset; NULLIF also maps '' to NULL.
-- A NULL comparison is never true, so with nothing set the API sees no rows at all (deny by default).
ALTER TABLE tenant_members ENABLE ROW LEVEL SECURITY;
-- FORCE applies the policies to the table owner too (superusers still bypass RLS).
ALTER TABLE tenant_members FORCE ROW LEVEL SECURITY;

-- Inside a shop: full access, but only to that shop's rows.
-- WITH CHECK also blocks inserting/moving a row into another shop.
CREATE POLICY tenant_isolation ON tenant_members
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Right after login, before a shop is chosen: a user may list their own memberships (read-only).
-- Permissive policies are OR-ed, so this adds rows; it never grants writes.
CREATE POLICY member_self_read ON tenant_members
    FOR SELECT
    USING (user_id = NULLIF(current_setting('app.user_id', true), '')::uuid);
