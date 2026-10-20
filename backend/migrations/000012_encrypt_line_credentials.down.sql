-- Going back loses the encrypted secrets (SQL cannot decrypt them): rows that only have an
-- encrypted secret are deleted, and those shops must connect LINE again.
DELETE FROM line_channels WHERE channel_secret IS NULL;

ALTER TABLE line_channels
    DROP CONSTRAINT line_channels_secret_present,
    DROP COLUMN bot_basic_id,
    DROP COLUMN bot_name,
    DROP COLUMN access_token_enc,
    DROP COLUMN channel_secret_enc,
    ALTER COLUMN channel_secret SET NOT NULL;
