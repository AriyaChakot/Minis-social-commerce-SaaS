// Package platform is the console of the people who run MSCS (platform admins): numbers across
// all shops, and suspending a shop that breaks the rules.
//
// Everything else in the API sees one shop at a time (Row-Level Security). The cross-shop numbers
// come from SECURITY DEFINER functions (migration 000013) that return counts and sums only.
// Platform admins are not shop members and get no access to a shop's orders or customers.
package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/database"
	"mscs/internal/tenant"
)

var (
	ErrTenantNotFound   = errors.New("platform: tenant not found")
	ErrAlreadySuspended = errors.New("platform: tenant is already suspended")
	ErrNotSuspended     = errors.New("platform: tenant is not suspended")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("platform: invalid %s: %s", e.Field, e.Message)
}

// Thailand has no daylight saving time, so a fixed UTC+7 zone is exact (and needs no tz database
// in the container image).
var bangkok = time.FixedZone("Asia/Bangkok", 7*60*60)

// OverviewDays is the length of the overview's daily series.
const OverviewDays = 30

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: time.Now}
}

// IsAdmin reports whether the user is a platform admin (a row in platform_admins, which only the
// database owner can write).
func (s *Service) IsAdmin(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform_admins WHERE user_id = $1)`, userID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("platform: is admin: %w", err)
	}
	return ok, nil
}

// Tenant is a shop as the platform sees it: who runs it, how big it is, its status.
type Tenant struct {
	ID              uuid.UUID  `json:"id"`
	Slug            string     `json:"slug"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	SuspendedReason string     `json:"suspended_reason"`
	SuspendedAt     *time.Time `json:"suspended_at"`
	CreatedAt       time.Time  `json:"created_at"`
	OwnerEmail      string     `json:"owner_email"`
	MemberCount     int64      `json:"member_count"`
	ProductCount    int64      `json:"product_count"` // on sale
	OrderCount      int64      `json:"order_count"`
	RevenueSatang   int64      `json:"revenue_satang"` // paid orders
	LastOrderAt     *time.Time `json:"last_order_at"`
}

const tenantQuery = `
	SELECT t.id, t.slug, t.name, t.status, t.suspended_reason, t.suspended_at, t.created_at,
	       coalesce(s.owner_email, ''), s.member_count, s.product_count, s.order_count,
	       s.revenue_satang, s.last_order_at
	FROM tenants t
	JOIN platform_tenant_stats() s ON s.tenant_id = t.id`

func scanTenant(row pgx.Row) (Tenant, error) {
	var t Tenant
	err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.SuspendedReason, &t.SuspendedAt, &t.CreatedAt,
		&t.OwnerEmail, &t.MemberCount, &t.ProductCount, &t.OrderCount, &t.RevenueSatang, &t.LastOrderAt)
	return t, err
}

// TenantFilter narrows the shop list. Empty fields mean "any".
type TenantFilter struct {
	Query  string // part of the name or slug
	Status string // active, suspended
}

// TenantLimit: the console lists the newest shops first, this many at most (MVP).
const TenantLimit = 200

func (s *Service) Tenants(ctx context.Context, f TenantFilter) ([]Tenant, error) {
	if f.Status != "" && f.Status != tenant.StatusActive && f.Status != tenant.StatusSuspended {
		return nil, &ValidationError{"status", "must be active or suspended"}
	}
	q := strings.TrimSpace(f.Query)
	rows, err := s.pool.Query(ctx, tenantQuery+`
		WHERE ($1 = '' OR t.name ILIKE $2 OR t.slug ILIKE $2)
		  AND ($3 = '' OR t.status = $3)
		ORDER BY t.created_at DESC
		LIMIT $4`,
		q, database.ContainsPattern(q), f.Status, TenantLimit)
	if err != nil {
		return nil, fmt.Errorf("platform: list tenants: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Tenant, error) { return scanTenant(row) })
	if err != nil {
		return nil, fmt.Errorf("platform: list tenants: %w", err)
	}
	if list == nil {
		list = []Tenant{}
	}
	return list, nil
}

func (s *Service) tenant(ctx context.Context, db database.DBTX, id uuid.UUID) (Tenant, error) {
	t, err := scanTenant(db.QueryRow(ctx, tenantQuery+` WHERE t.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrTenantNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("platform: get tenant: %w", err)
	}
	return t, nil
}

// Overview is the console's first page.
type Overview struct {
	Tenants struct {
		Total     int64 `json:"total"`
		Active    int64 `json:"active"`
		Suspended int64 `json:"suspended"`
	} `json:"tenants"`
	Users int64 `json:"users"`
	// Totals of the daily series below.
	Orders        int64        `json:"orders"`
	RevenueSatang int64        `json:"revenue_satang"`
	Daily         []DailyPoint `json:"daily"`
}

// DailyPoint is one Thai calendar day; days without orders are included with zeros.
type DailyPoint struct {
	Day           string `json:"day"` // 2026-10-20
	Orders        int64  `json:"orders"`
	RevenueSatang int64  `json:"revenue_satang"`
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE status = 'active'),
		       count(*) FILTER (WHERE status = 'suspended'),
		       (SELECT count(*) FROM users)
		FROM tenants`,
	).Scan(&o.Tenants.Total, &o.Tenants.Active, &o.Tenants.Suspended, &o.Users)
	if err != nil {
		return Overview{}, fmt.Errorf("platform: overview counts: %w", err)
	}

	// The last OverviewDays days, today included, starting at midnight Bangkok time.
	today := s.now().In(bangkok)
	first := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, bangkok).AddDate(0, 0, -(OverviewDays - 1))

	rows, err := s.pool.Query(ctx, `SELECT day, order_count, revenue_satang FROM platform_daily_orders($1)`, first)
	if err != nil {
		return Overview{}, fmt.Errorf("platform: daily orders: %w", err)
	}
	byDay := map[string]DailyPoint{}
	var day time.Time
	var p DailyPoint
	_, err = pgx.ForEachRow(rows, []any{&day, &p.Orders, &p.RevenueSatang}, func() error {
		p.Day = day.Format(time.DateOnly)
		byDay[p.Day] = p
		return nil
	})
	if err != nil {
		return Overview{}, fmt.Errorf("platform: daily orders: %w", err)
	}

	// Fill the gaps: a chart needs every day, not only the days that had orders.
	o.Daily = make([]DailyPoint, 0, OverviewDays)
	for i := range OverviewDays {
		key := first.AddDate(0, 0, i).Format(time.DateOnly)
		point := byDay[key]
		point.Day = key
		o.Daily = append(o.Daily, point)
		o.Orders += point.Orders
		o.RevenueSatang += point.RevenueSatang
	}
	return o, nil
}

// AuditEntry is one platform action.
type AuditEntry struct {
	ID         uuid.UUID `json:"id"`
	Action     string    `json:"action"` // tenant.suspend, tenant.reactivate
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
	ActorEmail string    `json:"actor_email"`
	TenantID   uuid.UUID `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
}

// AuditLimit: the console shows the latest actions only.
const AuditLimit = 20

func (s *Service) Audit(ctx context.Context) ([]AuditEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.action, a.reason, a.created_at, u.email, t.id, t.name
		FROM platform_audit_log a
		JOIN users u ON u.id = a.actor_id
		JOIN tenants t ON t.id = a.tenant_id
		ORDER BY a.id DESC
		LIMIT $1`, AuditLimit)
	if err != nil {
		return nil, fmt.Errorf("platform: audit log: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AuditEntry, error) {
		var e AuditEntry
		err := row.Scan(&e.ID, &e.Action, &e.Reason, &e.CreatedAt, &e.ActorEmail, &e.TenantID, &e.TenantName)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("platform: audit log: %w", err)
	}
	if list == nil {
		list = []AuditEntry{}
	}
	return list, nil
}

// Suspend closes a shop: its storefront disappears and its members are locked out of the
// merchant app. The reason is shown to them, and recorded with who did it.
func (s *Service) Suspend(ctx context.Context, actorID, tenantID uuid.UUID, reason string) (Tenant, error) {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 {
		return Tenant{}, &ValidationError{"reason", "must be 3 to 500 characters"}
	}
	return s.setStatus(ctx, actorID, tenantID, "tenant.suspend", reason,
		`UPDATE tenants SET status = 'suspended', suspended_at = now(), suspended_reason = $2
		 WHERE id = $1 AND status = 'active'`, ErrAlreadySuspended)
}

// Reactivate opens a suspended shop again.
func (s *Service) Reactivate(ctx context.Context, actorID, tenantID uuid.UUID) (Tenant, error) {
	return s.setStatus(ctx, actorID, tenantID, "tenant.reactivate", "",
		`UPDATE tenants SET status = 'active', suspended_at = NULL, suspended_reason = $2
		 WHERE id = $1 AND status = 'suspended'`, ErrNotSuspended)
}

// setStatus changes the shop and writes the audit entry in one transaction: there is never a
// suspension without its record, or a record of a suspension that did not happen.
func (s *Service) setStatus(ctx context.Context, actorID, tenantID uuid.UUID, action, reason, update string, wrongState error) (Tenant, error) {
	var out Tenant
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// The status condition in the UPDATE makes it a compare-and-set: two admins clicking at
		// the same time cannot both suspend (or both reactivate) the shop.
		tag, err := tx.Exec(ctx, update, tenantID, reason)
		if err != nil {
			return fmt.Errorf("platform: %s: %w", action, err)
		}
		if tag.RowsAffected() == 0 {
			if _, err := s.tenant(ctx, tx, tenantID); err != nil {
				return err // ErrTenantNotFound
			}
			return wrongState
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO platform_audit_log (actor_id, action, tenant_id, reason) VALUES ($1, $2, $3, $4)`,
			actorID, action, tenantID, reason)
		if err != nil {
			return fmt.Errorf("platform: audit %s: %w", action, err)
		}
		out, err = s.tenant(ctx, tx, tenantID)
		return err
	})
	return out, err
}
