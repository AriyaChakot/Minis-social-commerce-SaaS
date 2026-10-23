-- Inviting staff: an owner or admin creates an invitation for an email address, gets a link
-- once, and sends it to the person (by LINE, for example). Opening the link lets them join the
-- shop with an existing account or create one.
CREATE TABLE tenant_invitations (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    email       text        NOT NULL CHECK (email = lower(email) AND length(email) <= 254),
    -- Never 'owner': ownership is not handed out by link.
    role        text        NOT NULL CHECK (role IN ('admin', 'staff')),
    -- The link's token is shown once, at creation; like sessions, only its SHA-256 is kept, so
    -- a database leak does not leak working invitation links.
    token_hash  bytea       NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    invited_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz,
    accepted_by uuid        REFERENCES users (id) ON DELETE SET NULL,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT tenant_invitations_one_outcome CHECK (accepted_at IS NULL OR revoked_at IS NULL)
);

-- At most one open invitation per person per shop (inviting again replaces the old one).
CREATE UNIQUE INDEX tenant_invitations_open_key
    ON tenant_invitations (tenant_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

ALTER TABLE tenant_invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_invitations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_invitations
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Whoever opens a link is not a member yet, so RLS hides the invitation from them. This
-- function finds exactly one invitation by its token hash (knowing the hash = having the link)
-- and returns what the invitation page needs, nothing more.
CREATE FUNCTION invitation_by_token(hash bytea)
RETURNS TABLE (
    id            uuid,
    tenant_id     uuid,
    tenant_name   text,
    tenant_status text,
    email         text,
    role          text,
    expires_at    timestamptz,
    accepted_at   timestamptz,
    revoked_at    timestamptz
)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT i.id, i.tenant_id, t.name, t.status, i.email, i.role, i.expires_at, i.accepted_at, i.revoked_at
    FROM tenant_invitations i
    JOIN tenants t ON t.id = i.tenant_id
    WHERE i.token_hash = hash
$$;
REVOKE EXECUTE ON FUNCTION invitation_by_token(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION invitation_by_token(bytea) TO mscs_app;
