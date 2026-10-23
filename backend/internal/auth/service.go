package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/database"
	"mscs/internal/tenant"
	"mscs/internal/user"
)

var (
	// ErrInvalidCredentials is deliberately vague: it never says whether the email exists.
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	ErrUnauthenticated    = errors.New("auth: missing, invalid or expired session")
)

// ValidationError is a business-rule failure on one input field (maps to HTTP 400).
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("auth: invalid %s: %s", e.Field, e.Message)
}

// Same rule as the tenants_slug_format constraint in the database.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// dummyHash is compared against when an email does not exist, so a login for an unknown email
// takes as long as one with a wrong password (no timing leak about which emails are registered).
var dummyHash = HashPassword("dummy password used only to equalize timing")

// Service implements the auth use cases. Handlers call it; it calls the repositories.
type Service struct {
	pool     *pgxpool.Pool
	users    user.Repository
	tenants  tenant.Repository
	sessions sessionStore
	now      func() time.Time // replaceable in tests
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: time.Now}
}

type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
	ShopName    string
	ShopSlug    string
}

type RegisterResult struct {
	User    user.User     `json:"user"`
	Tenant  tenant.Tenant `json:"tenant"`
	Session Session       `json:"session"`
}

// Register creates a user, their first shop, the owner membership and a session,
// all in ONE transaction: if any part fails, nothing is saved.
func (s *Service) Register(ctx context.Context, in RegisterInput) (RegisterResult, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.ShopName = strings.TrimSpace(in.ShopName)
	in.ShopSlug = strings.ToLower(strings.TrimSpace(in.ShopSlug))

	switch {
	case in.DisplayName == "":
		return RegisterResult{}, &ValidationError{"display_name", "must not be blank"}
	case in.ShopName == "":
		return RegisterResult{}, &ValidationError{"shop_name", "must not be blank"}
	case !slugPattern.MatchString(in.ShopSlug):
		return RegisterResult{}, &ValidationError{"shop_slug", "use lowercase letters, digits and single hyphens"}
	}

	// Hash BEFORE opening the transaction: hashing is slow on purpose (~tens of ms),
	// and a transaction should never stay open longer than necessary.
	hash := HashPassword(in.Password)

	var res RegisterResult
	err := database.InTx(ctx, s.pool, database.Scope{}, func(tx pgx.Tx) error {
		u, err := s.users.Create(ctx, tx, in.Email, hash, in.DisplayName)
		if err != nil {
			return err
		}
		t, err := s.tenants.Create(ctx, tx, in.ShopSlug, in.ShopName)
		if err != nil {
			return err
		}
		// The tenant id exists only now, so switch the RLS scope inside the same transaction.
		if err := database.SetScope(ctx, tx, database.Scope{TenantID: t.ID, UserID: u.ID}); err != nil {
			return err
		}
		if err := s.tenants.AddMember(ctx, tx, t.ID, u.ID, tenant.RoleOwner); err != nil {
			return err
		}
		sess, err := s.createSession(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		res = RegisterResult{User: u, Tenant: t, Session: sess}
		return nil
	})
	return res, err
}

// Login checks the password and opens a new session.
func (s *Service) Login(ctx context.Context, email, password string) (user.User, Session, error) {
	u, err := s.users.GetByEmail(ctx, s.pool, email)
	if errors.Is(err, user.ErrNotFound) {
		_, _ = VerifyPassword(password, dummyHash)
		return user.User{}, Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return user.User{}, Session{}, err
	}

	ok, err := VerifyPassword(password, u.PasswordHash)
	if err != nil {
		return user.User{}, Session{}, err
	}
	if !ok {
		return user.User{}, Session{}, ErrInvalidCredentials
	}

	sess, err := s.createSession(ctx, s.pool, u.ID)
	if err != nil {
		return user.User{}, Session{}, err
	}
	return u, sess, nil
}

// Logout ends the session that owns token. Unknown tokens are ignored (logout is idempotent).
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.sessions.delete(ctx, s.pool, hashToken(token))
}

// Authenticate turns a session token into the logged-in user.
func (s *Service) Authenticate(ctx context.Context, token string) (user.User, error) {
	if token == "" {
		return user.User{}, ErrUnauthenticated
	}
	id, err := s.sessions.userID(ctx, s.pool, hashToken(token))
	if err != nil {
		return user.User{}, err
	}
	u, err := s.users.GetByID(ctx, s.pool, id)
	if errors.Is(err, user.ErrNotFound) {
		return user.User{}, ErrUnauthenticated
	}
	return u, err
}

// Memberships lists the tenants a user belongs to (shown after login to pick a shop).
func (s *Service) Memberships(ctx context.Context, userID uuid.UUID) ([]tenant.Membership, error) {
	var out []tenant.Membership
	err := database.InTx(ctx, s.pool, database.Scope{UserID: userID}, func(tx pgx.Tx) error {
		var err error
		out, err = s.tenants.ListMemberships(ctx, tx, userID)
		return err
	})
	return out, err
}

// TenantAccess returns the tenant and the user's role in it, or tenant.ErrNotMember.
// Every tenant-scoped API request goes through this check (see handler.RequireTenantMember).
func (s *Service) TenantAccess(ctx context.Context, tenantID, userID uuid.UUID) (tenant.Tenant, tenant.Role, error) {
	var (
		t    tenant.Tenant
		role tenant.Role
	)
	err := database.InTx(ctx, s.pool, database.Scope{TenantID: tenantID, UserID: userID}, func(tx pgx.Tx) error {
		var err error
		if role, err = s.tenants.MemberRole(ctx, tx, tenantID, userID); err != nil {
			return err
		}
		t, err = s.tenants.GetByID(ctx, tx, tenantID)
		return err
	})
	return t, role, err
}

// CreateSession opens a session for a user inside the caller's transaction (used by flows that
// create the account elsewhere, such as accepting an invitation with a new account).
func (s *Service) CreateSession(ctx context.Context, db database.DBTX, userID uuid.UUID) (Session, error) {
	return s.createSession(ctx, db, userID)
}

func (s *Service) createSession(ctx context.Context, db database.DBTX, userID uuid.UUID) (Session, error) {
	token := newToken()
	expiresAt := s.now().Add(SessionTTL).UTC()
	if err := s.sessions.create(ctx, db, userID, hashToken(token), expiresAt); err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: expiresAt}, nil
}

// sessionCleanupBatch is how many expired sessions one DELETE removes.
const sessionCleanupBatch = 1000

// DeleteExpiredSessions removes sessions that can no longer be used. Expired sessions are already
// rejected on every request (expires_at > now()); this only keeps the table from growing forever.
func (s *Service) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	cutoff := s.now()
	var total int64
	for {
		n, err := s.sessions.deleteExpired(ctx, s.pool, cutoff, sessionCleanupBatch)
		total += n
		if err != nil || n < sessionCleanupBatch {
			return total, err
		}
	}
}
