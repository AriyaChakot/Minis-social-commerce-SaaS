-- A shop's connection to its LINE Official Account. LINE signs every webhook it sends with the
-- channel secret (HMAC-SHA256); we keep the secret to check that a webhook really comes from LINE.
--
-- No RLS, like sessions: a webhook must find its channel (and so its shop) before anyone knows
-- which shop is involved. Queries from the merchant app still filter on tenant_id explicitly,
-- and the API never returns the secret.
CREATE TABLE line_channels (
    -- Part of the webhook URL the merchant pastes into the LINE console.
    id             uuid        PRIMARY KEY DEFAULT uuidv7(),
    -- One LINE Official Account per shop in this version.
    tenant_id      uuid        NOT NULL UNIQUE REFERENCES tenants (id) ON DELETE CASCADE,
    -- LINE channel secrets are 32 lowercase hex characters.
    channel_secret text        NOT NULL CHECK (channel_secret ~ '^[0-9a-f]{32}$'),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER line_channels_set_updated_at
    BEFORE UPDATE ON line_channels
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
