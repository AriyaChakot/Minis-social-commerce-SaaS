-- Server-side login sessions. The client holds a random token; we store only its SHA-256 hash,
-- so a leaked database (or backup) cannot be used to log in as anyone.
-- No RLS here: a session is looked up by token before we know who the user is.
CREATE TABLE sessions (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea       NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,

    CONSTRAINT sessions_token_hash_len CHECK (length(token_hash) = 32),
    CONSTRAINT sessions_expiry_after_creation CHECK (expires_at > created_at)
);

-- "Log out everywhere" and cleanup jobs look sessions up by user.
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
