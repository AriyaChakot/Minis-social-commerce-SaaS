package inbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mscs/internal/database"
)

// lineChannelRow is a line_channels row, secret included: it never leaves this package.
type lineChannelRow struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Secret    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (r lineChannelRow) public() LineChannel {
	return LineChannel{
		ID:          r.ID,
		WebhookPath: "/api/v1/webhooks/line/" + r.ID.String(),
		SecretHint:  "…" + r.Secret[len(r.Secret)-4:],
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// channelRepo reads and writes line_channels (no RLS; see migration 000011).
type channelRepo struct{}

const lineChannelColumns = `id, tenant_id, channel_secret, created_at, updated_at`

func scanLineChannel(row pgx.Row) (lineChannelRow, error) {
	var r lineChannelRow
	err := row.Scan(&r.ID, &r.TenantID, &r.Secret, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return lineChannelRow{}, ErrChannelNotFound
	}
	if err != nil {
		return lineChannelRow{}, fmt.Errorf("inbox: line channel: %w", err)
	}
	return r, nil
}

// byID is the webhook's lookup: the id comes from the URL, the shop is not known yet.
func (channelRepo) byID(ctx context.Context, db database.DBTX, id uuid.UUID) (lineChannelRow, error) {
	return scanLineChannel(db.QueryRow(ctx, `SELECT `+lineChannelColumns+` FROM line_channels WHERE id = $1`, id))
}

func (channelRepo) byTenant(ctx context.Context, db database.DBTX, tenantID uuid.UUID) (lineChannelRow, error) {
	return scanLineChannel(db.QueryRow(ctx, `SELECT `+lineChannelColumns+` FROM line_channels WHERE tenant_id = $1`, tenantID))
}

// upsert connects the shop, or replaces its secret (e.g. after the merchant reissued it in the
// LINE console). The id, and so the webhook URL, stays the same.
func (channelRepo) upsert(ctx context.Context, db database.DBTX, tenantID uuid.UUID, secret string) (lineChannelRow, error) {
	return scanLineChannel(db.QueryRow(ctx,
		`INSERT INTO line_channels (tenant_id, channel_secret) VALUES ($1, $2)
		 ON CONFLICT (tenant_id) DO UPDATE SET channel_secret = EXCLUDED.channel_secret
		 RETURNING `+lineChannelColumns,
		tenantID, secret,
	))
}
