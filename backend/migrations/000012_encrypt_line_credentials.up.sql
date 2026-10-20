-- Encrypt LINE credentials at rest, and add the channel access token (needed to reply to
-- customers and read their profile names).
--
-- The values are encrypted by the API (AES-256-GCM, package secretbox) with a key that only the
-- API has (DATA_ENCRYPTION_KEY), so a database dump or backup no longer contains usable secrets.
--
-- "Expand, migrate, contract": SQL cannot encrypt here (the key is not in the database), so this
-- migration only EXPANDS the table. The API encrypts the existing plaintext secrets when it
-- starts (inbox.Service.EncryptLegacySecrets) and clears channel_secret. A later migration can
-- CONTRACT by dropping the channel_secret column once every environment has run that.
ALTER TABLE line_channels
    ALTER COLUMN channel_secret DROP NOT NULL,
    ADD COLUMN channel_secret_enc bytea,
    -- Optional: without it the inbox receives messages but cannot reply.
    ADD COLUMN access_token_enc   bytea,
    -- Shown to the merchant to confirm which account the token belongs to (not secret).
    ADD COLUMN bot_name           text NOT NULL DEFAULT '',
    ADD COLUMN bot_basic_id       text NOT NULL DEFAULT '',
    -- Every row keeps its secret, in one form or the other.
    ADD CONSTRAINT line_channels_secret_present
        CHECK (channel_secret IS NOT NULL OR channel_secret_enc IS NOT NULL);
