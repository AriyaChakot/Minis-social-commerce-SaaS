package inbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mscs/internal/database"
	"mscs/internal/secretbox"
)

// lineChannelRow is a line_channels row with its credentials decrypted: it never leaves this package.
type lineChannelRow struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	Secret     string
	Token      string // empty when the shop has not added an access token
	BotName    string
	BotBasicID string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (r lineChannelRow) public() LineChannel {
	return LineChannel{
		ID:          r.ID,
		WebhookPath: "/api/v1/webhooks/line/" + r.ID.String(),
		SecretHint:  "…" + r.Secret[len(r.Secret)-4:],
		CanReply:    r.Token != "",
		BotName:     r.BotName,
		BotBasicID:  r.BotBasicID,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// channelRepo reads and writes line_channels (no RLS; see migration 000011). Credentials are
// encrypted on the way in and decrypted on the way out, so plaintext never reaches the database.
type channelRepo struct {
	box *secretbox.Box
}

// The encryption context ties each value to its shop and column: a ciphertext copied into
// another row or column does not decrypt there.
func secretContext(tenantID uuid.UUID) string {
	return "line_channels/" + tenantID.String() + "/channel_secret"
}
func tokenContext(tenantID uuid.UUID) string {
	return "line_channels/" + tenantID.String() + "/access_token"
}

const lineChannelColumns = `id, tenant_id, channel_secret, channel_secret_enc, access_token_enc,
	bot_name, bot_basic_id, created_at, updated_at`

func (repo channelRepo) scan(row pgx.Row) (lineChannelRow, error) {
	var (
		r                   lineChannelRow
		legacySecret        *string
		secretEnc, tokenEnc []byte
	)
	err := row.Scan(&r.ID, &r.TenantID, &legacySecret, &secretEnc, &tokenEnc,
		&r.BotName, &r.BotBasicID, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return lineChannelRow{}, ErrChannelNotFound
	}
	if err != nil {
		return lineChannelRow{}, fmt.Errorf("inbox: line channel: %w", err)
	}

	switch {
	case secretEnc != nil:
		secret, err := repo.box.Open(secretEnc, secretContext(r.TenantID))
		if err != nil {
			return lineChannelRow{}, fmt.Errorf("inbox: line channel %s secret: %w", r.ID, err)
		}
		r.Secret = string(secret)
	case legacySecret != nil:
		r.Secret = *legacySecret // not encrypted yet (before EncryptLegacySecrets ran)
	}
	if tokenEnc != nil {
		token, err := repo.box.Open(tokenEnc, tokenContext(r.TenantID))
		if err != nil {
			return lineChannelRow{}, fmt.Errorf("inbox: line channel %s token: %w", r.ID, err)
		}
		r.Token = string(token)
	}
	return r, nil
}

// byID is the webhook's lookup: the id comes from the URL, the shop is not known yet.
func (repo channelRepo) byID(ctx context.Context, db database.DBTX, id uuid.UUID) (lineChannelRow, error) {
	return repo.scan(db.QueryRow(ctx, `SELECT `+lineChannelColumns+` FROM line_channels WHERE id = $1`, id))
}

func (repo channelRepo) byTenant(ctx context.Context, db database.DBTX, tenantID uuid.UUID) (lineChannelRow, error) {
	return repo.scan(db.QueryRow(ctx, `SELECT `+lineChannelColumns+` FROM line_channels WHERE tenant_id = $1`, tenantID))
}

// upsertSecret connects the shop, or replaces its secret (e.g. after the merchant reissued it in
// the LINE console). The id, and so the webhook URL, stays the same.
func (repo channelRepo) upsertSecret(ctx context.Context, db database.DBTX, tenantID uuid.UUID, secret string) (lineChannelRow, error) {
	return repo.scan(db.QueryRow(ctx,
		`INSERT INTO line_channels (tenant_id, channel_secret_enc) VALUES ($1, $2)
		 ON CONFLICT (tenant_id) DO UPDATE
		   SET channel_secret_enc = EXCLUDED.channel_secret_enc, channel_secret = NULL
		 RETURNING `+lineChannelColumns,
		tenantID, repo.box.Seal([]byte(secret), secretContext(tenantID)),
	))
}

// setToken saves the access token of a connected shop, with the account it belongs to.
func (repo channelRepo) setToken(ctx context.Context, db database.DBTX, tenantID uuid.UUID, token, botName, botBasicID string) (lineChannelRow, error) {
	return repo.scan(db.QueryRow(ctx,
		`UPDATE line_channels SET access_token_enc = $2, bot_name = $3, bot_basic_id = $4
		 WHERE tenant_id = $1
		 RETURNING `+lineChannelColumns,
		tenantID, repo.box.Seal([]byte(token), tokenContext(tenantID)), botName, botBasicID,
	))
}

// encryptLegacy encrypts the secrets stored in plaintext before migration 000012, and reports
// how many rows it changed. Safe to run again: it only touches rows that still have plaintext.
func (repo channelRepo) encryptLegacy(ctx context.Context, db database.DBTX) (int, error) {
	rows, err := db.Query(ctx, `SELECT tenant_id, channel_secret FROM line_channels WHERE channel_secret IS NOT NULL`)
	if err != nil {
		return 0, fmt.Errorf("inbox: find plaintext secrets: %w", err)
	}
	type legacy struct {
		tenantID uuid.UUID
		secret   string
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (legacy, error) {
		var l legacy
		return l, row.Scan(&l.tenantID, &l.secret)
	})
	if err != nil {
		return 0, fmt.Errorf("inbox: find plaintext secrets: %w", err)
	}

	done := 0
	for _, l := range list {
		// "AND channel_secret = $3": if the merchant saved a new secret meanwhile, leave it alone.
		tag, err := db.Exec(ctx,
			`UPDATE line_channels SET channel_secret_enc = $2, channel_secret = NULL
			 WHERE tenant_id = $1 AND channel_secret = $3`,
			l.tenantID, repo.box.Seal([]byte(l.secret), secretContext(l.tenantID)), l.secret)
		if err != nil {
			return done, fmt.Errorf("inbox: encrypt secret: %w", err)
		}
		done += int(tag.RowsAffected())
	}
	return done, nil
}
