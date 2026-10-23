// Package team manages who works in a shop: its members, and invitations for new ones.
//
// There is no email sending in this version: creating an invitation returns a link, shown once,
// that the owner sends to the person themselves (LINE, chat...). Like a session token, only the
// link token's hash is stored.
package team

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/auth"
	"mscs/internal/database"
	"mscs/internal/securetoken"
	"mscs/internal/tenant"
	"mscs/internal/user"
)

// InvitationTTL: how long a link stays valid.
const InvitationTTL = 7 * 24 * time.Hour

var (
	ErrInvitationNotFound = errors.New("team: invitation not found") // unknown or revoked
	ErrInvitationExpired  = errors.New("team: invitation expired")
	ErrInvitationUsed     = errors.New("team: invitation already used")
	ErrShopUnavailable    = errors.New("team: the shop is suspended")
	ErrWrongAccount       = errors.New("team: the invitation is for another email address")
	ErrAlreadyMember      = errors.New("team: already a member of this shop")
	ErrMemberNotFound     = errors.New("team: member not found")
	// ErrNotAllowed: the actor's role cannot do this (e.g. an admin inviting an admin).
	ErrNotAllowed = errors.New("team: your role cannot do this")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("team: invalid %s: %s", e.Field, e.Message)
}

type Member struct {
	UserID      uuid.UUID   `json:"user_id"`
	Email       string      `json:"email"`
	DisplayName string      `json:"display_name"`
	Role        tenant.Role `json:"role"`
	JoinedAt    time.Time   `json:"joined_at"`
}

// Invitation is a pending invitation as the shop's managers see it (never the token).
type Invitation struct {
	ID        uuid.UUID   `json:"id"`
	Email     string      `json:"email"`
	Role      tenant.Role `json:"role"`
	ExpiresAt time.Time   `json:"expires_at"`
	CreatedAt time.Time   `json:"created_at"`
}

// Preview is what the invitation page shows to whoever opens the link.
type Preview struct {
	TenantID   uuid.UUID   `json:"tenant_id"`
	TenantName string      `json:"tenant_name"`
	Email      string      `json:"email"`
	Role       tenant.Role `json:"role"`
	ExpiresAt  time.Time   `json:"expires_at"`
}

type Service struct {
	pool *pgxpool.Pool
	auth *auth.Service
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool, authSvc *auth.Service) *Service {
	return &Service{pool: pool, auth: authSvc, now: time.Now}
}

func (s *Service) inTenant(ctx context.Context, tenantID uuid.UUID, fn func(tx pgx.Tx) error) error {
	return database.InTx(ctx, s.pool, database.Scope{TenantID: tenantID}, fn)
}

// Team lists a shop's members (owners first) and its pending, unexpired invitations.
func (s *Service) Team(ctx context.Context, tenantID uuid.UUID) ([]Member, []Invitation, error) {
	members := []Member{}
	invitations := []Invitation{}
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.email, u.display_name, m.role, m.created_at
			FROM tenant_members m JOIN users u ON u.id = m.user_id
			WHERE m.tenant_id = $1
			ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, m.created_at`, tenantID)
		if err != nil {
			return fmt.Errorf("team: members: %w", err)
		}
		if members, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Member, error) {
			var m Member
			return m, r.Scan(&m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.JoinedAt)
		}); err != nil {
			return fmt.Errorf("team: members: %w", err)
		}

		rows, err = tx.Query(ctx, `
			SELECT id, email, role, expires_at, created_at FROM tenant_invitations
			WHERE tenant_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $2
			ORDER BY created_at DESC`, tenantID, s.now())
		if err != nil {
			return fmt.Errorf("team: invitations: %w", err)
		}
		invitations, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Invitation, error) {
			var i Invitation
			return i, r.Scan(&i.ID, &i.Email, &i.Role, &i.ExpiresAt, &i.CreatedAt)
		})
		return err
	})
	if invitations == nil {
		invitations = []Invitation{}
	}
	return members, invitations, err
}

// canManage: owners manage admins and staff; admins manage staff only. Nobody manages owners.
func canManage(actor, target tenant.Role) bool {
	switch actor {
	case tenant.RoleOwner:
		return target == tenant.RoleAdmin || target == tenant.RoleStaff
	case tenant.RoleAdmin:
		return target == tenant.RoleStaff
	}
	return false
}

// Invite creates an invitation and returns it with its token (the link), which is not stored
// and cannot be shown again. Inviting an email that already has an open invitation replaces it.
func (s *Service) Invite(ctx context.Context, tenantID, actorID uuid.UUID, actorRole tenant.Role, email string, role tenant.Role) (Invitation, string, error) {
	email = user.NormalizeEmail(email)
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 254 {
		return Invitation{}, "", &ValidationError{"email", "must be an email address"}
	}
	if role != tenant.RoleAdmin && role != tenant.RoleStaff {
		return Invitation{}, "", &ValidationError{"role", "must be admin or staff"}
	}
	if !canManage(actorRole, role) {
		return Invitation{}, "", ErrNotAllowed
	}

	token := securetoken.New()
	var inv Invitation
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var member bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM tenant_members m JOIN users u ON u.id = m.user_id
			               WHERE m.tenant_id = $1 AND u.email = $2)`, tenantID, email).Scan(&member); err != nil {
			return fmt.Errorf("team: check member: %w", err)
		}
		if member {
			return ErrAlreadyMember
		}
		// Inviting again: the old link stops working, the new one replaces it.
		if _, err := tx.Exec(ctx, `
			UPDATE tenant_invitations SET revoked_at = $3
			WHERE tenant_id = $1 AND email = $2 AND accepted_at IS NULL AND revoked_at IS NULL`,
			tenantID, email, s.now()); err != nil {
			return fmt.Errorf("team: replace invitation: %w", err)
		}
		return tx.QueryRow(ctx, `
			INSERT INTO tenant_invitations (tenant_id, email, role, token_hash, invited_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, email, role, expires_at, created_at`,
			tenantID, email, role, securetoken.Hash(token), actorID, s.now().Add(InvitationTTL),
		).Scan(&inv.ID, &inv.Email, &inv.Role, &inv.ExpiresAt, &inv.CreatedAt)
	})
	if err != nil {
		return Invitation{}, "", err
	}
	return inv, token, nil
}

// Revoke cancels a pending invitation: its link stops working.
func (s *Service) Revoke(ctx context.Context, tenantID uuid.UUID, actorRole tenant.Role, invitationID uuid.UUID) error {
	return s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var role tenant.Role
		err := tx.QueryRow(ctx, `
			SELECT role FROM tenant_invitations
			WHERE tenant_id = $1 AND id = $2 AND accepted_at IS NULL AND revoked_at IS NULL`,
			tenantID, invitationID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvitationNotFound
		}
		if err != nil {
			return fmt.Errorf("team: find invitation: %w", err)
		}
		if !canManage(actorRole, role) {
			return ErrNotAllowed
		}
		_, err = tx.Exec(ctx, `UPDATE tenant_invitations SET revoked_at = $3 WHERE tenant_id = $1 AND id = $2`,
			tenantID, invitationID, s.now())
		return err
	})
}

// RemoveMember takes a person out of the shop. Their sessions stay valid for their other shops,
// but every request to this shop checks membership, so the removal takes effect at once.
func (s *Service) RemoveMember(ctx context.Context, tenantID, actorID uuid.UUID, actorRole tenant.Role, userID uuid.UUID) error {
	if userID == actorID {
		return ErrNotAllowed // not your own account: another manager must do it
	}
	return s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var role tenant.Role
		err := tx.QueryRow(ctx, `SELECT role FROM tenant_members WHERE tenant_id = $1 AND user_id = $2 FOR UPDATE`,
			tenantID, userID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMemberNotFound
		}
		if err != nil {
			return fmt.Errorf("team: find member: %w", err)
		}
		if !canManage(actorRole, role) {
			return ErrNotAllowed
		}
		_, err = tx.Exec(ctx, `DELETE FROM tenant_members WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID)
		return err
	})
}

// openInvitation is an invitation found by its link token.
type openInvitation struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Preview  Preview
}

// byToken finds the invitation of a link and checks it can still be used.
func (s *Service) byToken(ctx context.Context, db database.DBTX, token string) (openInvitation, error) {
	if token == "" {
		return openInvitation{}, ErrInvitationNotFound
	}
	var (
		inv               openInvitation
		status            string
		accepted, revoked *time.Time
	)
	err := db.QueryRow(ctx, `
		SELECT id, tenant_id, tenant_name, tenant_status, email, role, expires_at, accepted_at, revoked_at
		FROM invitation_by_token($1)`, securetoken.Hash(token),
	).Scan(&inv.ID, &inv.TenantID, &inv.Preview.TenantName, &status, &inv.Preview.Email, &inv.Preview.Role,
		&inv.Preview.ExpiresAt, &accepted, &revoked)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return openInvitation{}, ErrInvitationNotFound
	case err != nil:
		return openInvitation{}, fmt.Errorf("team: find invitation: %w", err)
	case revoked != nil:
		return openInvitation{}, ErrInvitationNotFound
	case accepted != nil:
		return openInvitation{}, ErrInvitationUsed
	case !inv.Preview.ExpiresAt.After(s.now()):
		return openInvitation{}, ErrInvitationExpired
	case status != tenant.StatusActive:
		return openInvitation{}, ErrShopUnavailable
	}
	inv.Preview.TenantID = inv.TenantID
	return inv, nil
}

// Preview describes the invitation of a link (public: having the link is the permission).
func (s *Service) Preview(ctx context.Context, token string) (Preview, error) {
	inv, err := s.byToken(ctx, s.pool, token)
	return inv.Preview, err
}

// join adds the user to the shop and marks the invitation used, in the caller's transaction.
// The UPDATE is a compare-and-set: if the link is used twice at the same moment, one of the two
// finds it already accepted and fails.
func (s *Service) join(ctx context.Context, tx pgx.Tx, inv openInvitation, userID uuid.UUID) error {
	if err := database.SetScope(ctx, tx, database.Scope{TenantID: inv.TenantID, UserID: userID}); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE tenant_invitations SET accepted_at = $3, accepted_by = $4
		WHERE tenant_id = $1 AND id = $2 AND accepted_at IS NULL AND revoked_at IS NULL`,
		inv.TenantID, inv.ID, s.now(), userID)
	if err != nil {
		return fmt.Errorf("team: accept invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvitationUsed
	}
	err = tenant.Repository{}.AddMember(ctx, tx, inv.TenantID, userID, inv.Preview.Role)
	if errors.Is(err, tenant.ErrAlreadyMember) {
		return ErrAlreadyMember
	}
	return err
}

// Accept lets a logged-in user join the shop of an invitation sent to their email address.
func (s *Service) Accept(ctx context.Context, token string, u user.User) (Preview, error) {
	var p Preview
	err := database.InTx(ctx, s.pool, database.Scope{}, func(tx pgx.Tx) error {
		inv, err := s.byToken(ctx, tx, token)
		if err != nil {
			return err
		}
		// The link was sent to one person: someone it was forwarded to cannot use it with their
		// own account. (Without email verification this is a guard against mistakes, not proof.)
		if inv.Preview.Email != u.Email {
			return ErrWrongAccount
		}
		p = inv.Preview
		return s.join(ctx, tx, inv, u.ID)
	})
	return p, err
}

// RegisterInput creates an account for the invited email address.
type RegisterInput struct {
	DisplayName string
	Password    string
}

// Register creates the invited person's account, adds them to the shop and logs them in, all in
// one transaction. The email is the invitation's: it cannot be chosen.
func (s *Service) Register(ctx context.Context, token string, in RegisterInput) (user.User, Preview, auth.Session, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return user.User{}, Preview{}, auth.Session{}, &ValidationError{"display_name", "must not be blank"}
	}
	// Check the link before the slow password hash (and before keeping a transaction open).
	if _, err := s.Preview(ctx, token); err != nil {
		return user.User{}, Preview{}, auth.Session{}, err
	}
	hash := auth.HashPassword(in.Password)

	var (
		u    user.User
		p    Preview
		sess auth.Session
	)
	err := database.InTx(ctx, s.pool, database.Scope{}, func(tx pgx.Tx) error {
		inv, err := s.byToken(ctx, tx, token)
		if err != nil {
			return err
		}
		if u, err = (user.Repository{}).Create(ctx, tx, inv.Preview.Email, hash, in.DisplayName); err != nil {
			return err // user.ErrEmailTaken: they already have an account and should log in
		}
		if err := s.join(ctx, tx, inv, u.ID); err != nil {
			return err
		}
		p = inv.Preview
		sess, err = s.auth.CreateSession(ctx, tx, u.ID)
		return err
	})
	if err != nil {
		return user.User{}, Preview{}, auth.Session{}, err
	}
	return u, p, sess, nil
}
