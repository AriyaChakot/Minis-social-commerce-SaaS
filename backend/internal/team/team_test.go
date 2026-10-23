package team_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/auth"
	"mscs/internal/team"
	"mscs/internal/tenant"
	"mscs/internal/testutil/dbtest"
	"mscs/internal/user"
)

type fixture struct {
	// root is the top-level test: cleanups registered on it run when the whole test ends, so data
	// created in one subtest is still there for the next ones (a subtest's own t.Cleanup would
	// delete it as soon as that subtest finishes).
	root  *testing.T
	ctx   context.Context
	pool  *pgxpool.Pool
	owner *pgxpool.Pool
	auth  *auth.Service
	svc   *team.Service
}

// cleanupUser deletes a user created during the test (and, by cascade, their memberships).
func (f fixture) cleanupUser(email string) {
	f.root.Cleanup(func() { _, _ = f.owner.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email) })
}

// merchant registers an account with its own shop.
func (f fixture) merchant(t *testing.T) auth.RegisterResult {
	t.Helper()
	sfx := dbtest.Suffix()
	email := "team-" + sfx + "@example.com"
	res, err := f.auth.Register(f.ctx, auth.RegisterInput{Email: email, Password: "password-123",
		DisplayName: "Owner " + sfx, ShopName: "Team Test " + sfx, ShopSlug: "team-test-" + sfx})
	if err != nil {
		t.Fatal(err)
	}
	f.root.Cleanup(func() { _, _ = f.owner.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, res.Tenant.ID) })
	f.cleanupUser(email)
	return res
}

func (f fixture) roleIn(t *testing.T, tenantID, userID uuid.UUID) (tenant.Role, error) {
	t.Helper()
	_, role, err := f.auth.TenantAccess(f.ctx, tenantID, userID)
	return role, err
}

func TestInvitations(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	f := fixture{root: t, ctx: ctx, pool: pool, owner: dbtest.OwnerPool(t), auth: auth.NewService(pool)}
	f.svc = team.NewService(pool, f.auth)

	shop := f.merchant(t)   // the shop that invites
	friend := f.merchant(t) // someone with an account (and a shop) of their own
	shopID, ownerID := shop.Tenant.ID, shop.User.ID

	t.Run("who may invite whom", func(t *testing.T) {
		var verr *team.ValidationError
		if _, _, err := f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, "not an email", tenant.RoleStaff); !errors.As(err, &verr) {
			t.Fatalf("bad email: want a validation error, got %v", err)
		}
		if _, _, err := f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, "x@example.com", tenant.RoleOwner); !errors.As(err, &verr) {
			t.Fatalf("owner role: want a validation error, got %v", err)
		}
		if _, _, err := f.svc.Invite(ctx, shopID, ownerID, tenant.RoleAdmin, "x@example.com", tenant.RoleAdmin); !errors.Is(err, team.ErrNotAllowed) {
			t.Fatalf("an admin inviting an admin: want ErrNotAllowed, got %v", err)
		}
		if _, _, err := f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, shop.User.Email, tenant.RoleStaff); !errors.Is(err, team.ErrAlreadyMember) {
			t.Fatalf("inviting a member: want ErrAlreadyMember, got %v", err)
		}
	})

	t.Run("an existing account accepts; the link then stops working", func(t *testing.T) {
		_, old, err := f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, "  "+friend.User.Email+" ", tenant.RoleStaff)
		if err != nil {
			t.Fatal(err)
		}
		// Inviting again replaces the first link.
		inv, token, err := f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, friend.User.Email, tenant.RoleAdmin)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Preview(ctx, old); !errors.Is(err, team.ErrInvitationNotFound) {
			t.Fatalf("replaced link: want ErrInvitationNotFound, got %v", err)
		}
		_, pending, _ := f.svc.Team(ctx, shopID)
		if len(pending) != 1 || pending[0].ID != inv.ID || pending[0].Role != tenant.RoleAdmin {
			t.Fatalf("want one pending admin invitation, got %+v", pending)
		}

		p, err := f.svc.Preview(ctx, token)
		if err != nil || p.TenantID != shopID || p.Email != friend.User.Email || p.TenantName != shop.Tenant.Name {
			t.Fatalf("preview: got %+v (err %v)", p, err)
		}
		// Someone else, logged in with their own account, cannot use a link sent to the friend.
		if _, err := f.svc.Accept(ctx, token, shop.User); !errors.Is(err, team.ErrWrongAccount) {
			t.Fatalf("wrong account: want ErrWrongAccount, got %v", err)
		}
		if _, err := f.svc.Accept(ctx, token, friend.User); err != nil {
			t.Fatal(err)
		}
		if role, err := f.roleIn(t, shopID, friend.User.ID); err != nil || role != tenant.RoleAdmin {
			t.Fatalf("want the friend to be an admin, got %q (err %v)", role, err)
		}
		if _, err := f.svc.Accept(ctx, token, friend.User); !errors.Is(err, team.ErrInvitationUsed) {
			t.Fatalf("second use: want ErrInvitationUsed, got %v", err)
		}
	})

	t.Run("a new person registers through the link", func(t *testing.T) {
		email := "new-" + dbtest.Suffix() + "@example.com"
		f.cleanupUser(email)
		_, token, err := f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, email, tenant.RoleStaff)
		if err != nil {
			t.Fatal(err)
		}
		var verr *team.ValidationError
		if _, _, _, err := f.svc.Register(ctx, token, team.RegisterInput{DisplayName: " ", Password: "password-123"}); !errors.As(err, &verr) {
			t.Fatalf("blank name: want a validation error, got %v", err)
		}
		u, p, sess, err := f.svc.Register(ctx, token, team.RegisterInput{DisplayName: " Nok ", Password: "password-123"})
		if err != nil {
			t.Fatal(err)
		}
		if u.Email != email || u.DisplayName != "Nok" || p.TenantID != shopID {
			t.Fatalf("got %+v / %+v", u, p)
		}
		// Logged in right away, and a member.
		if me, err := f.auth.Authenticate(ctx, sess.Token); err != nil || me.ID != u.ID {
			t.Fatalf("session: got %v (err %v)", me.ID, err)
		}
		if role, err := f.roleIn(t, shopID, u.ID); err != nil || role != tenant.RoleStaff {
			t.Fatalf("want staff, got %q (err %v)", role, err)
		}
		if _, _, _, err := f.svc.Register(ctx, token, team.RegisterInput{DisplayName: "Again", Password: "password-123"}); !errors.Is(err, team.ErrInvitationUsed) {
			t.Fatalf("second use: want ErrInvitationUsed, got %v", err)
		}
	})

	t.Run("registering with an email that has an account: log in instead", func(t *testing.T) {
		other := f.merchant(t)
		_, token, err := f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, other.User.Email, tenant.RoleStaff)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := f.svc.Register(ctx, token, team.RegisterInput{DisplayName: "X", Password: "password-123"}); !errors.Is(err, user.ErrEmailTaken) {
			t.Fatalf("want ErrEmailTaken, got %v", err)
		}
		// Nothing changed: the invitation is still open.
		if _, err := f.svc.Preview(ctx, token); err != nil {
			t.Fatalf("the failed registration must not use the link: %v", err)
		}
	})

	t.Run("expired, revoked and suspended-shop links", func(t *testing.T) {
		inv, token, _ := f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, "late-"+dbtest.Suffix()+"@example.com", tenant.RoleStaff)
		if _, err := f.owner.Exec(ctx, `UPDATE tenant_invitations SET expires_at = now() - interval '1 minute' WHERE id = $1`, inv.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Preview(ctx, token); !errors.Is(err, team.ErrInvitationExpired) {
			t.Fatalf("expired: want ErrInvitationExpired, got %v", err)
		}

		inv, token, _ = f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, "gone-"+dbtest.Suffix()+"@example.com", tenant.RoleStaff)
		if err := f.svc.Revoke(ctx, shopID, tenant.RoleOwner, inv.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Preview(ctx, token); !errors.Is(err, team.ErrInvitationNotFound) {
			t.Fatalf("revoked: want ErrInvitationNotFound, got %v", err)
		}
		if err := f.svc.Revoke(ctx, friend.Tenant.ID, tenant.RoleOwner, inv.ID); !errors.Is(err, team.ErrInvitationNotFound) {
			t.Fatalf("revoke from another shop: want not found, got %v", err)
		}

		_, token, _ = f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, "closed-"+dbtest.Suffix()+"@example.com", tenant.RoleStaff)
		if _, err := f.owner.Exec(ctx, `UPDATE tenants SET status = 'suspended', suspended_at = now() WHERE id = $1`, shopID); err != nil {
			t.Fatal(err)
		}
		_, err := f.svc.Preview(ctx, token)
		_, _ = f.owner.Exec(ctx, `UPDATE tenants SET status = 'active', suspended_at = NULL WHERE id = $1`, shopID)
		if !errors.Is(err, team.ErrShopUnavailable) {
			t.Fatalf("suspended shop: want ErrShopUnavailable, got %v", err)
		}
	})

	t.Run("a link used twice at the same moment works once", func(t *testing.T) {
		other := f.merchant(t)
		_, token, _ := f.svc.Invite(ctx, shopID, ownerID, tenant.RoleOwner, other.User.Email, tenant.RoleStaff)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for range 2 {
			wg.Go(func() { _, err := f.svc.Accept(ctx, token, other.User); errs <- err })
		}
		wg.Wait()
		close(errs)
		ok := 0
		for err := range errs {
			if err == nil {
				ok++
			} else if !errors.Is(err, team.ErrInvitationUsed) && !errors.Is(err, team.ErrAlreadyMember) {
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if ok != 1 {
			t.Fatalf("want exactly one success, got %d", ok)
		}
	})

	t.Run("removing members", func(t *testing.T) {
		members, _, _ := f.svc.Team(ctx, shopID)
		var staff, admin uuid.UUID
		for _, m := range members {
			switch m.Role {
			case tenant.RoleStaff:
				staff = m.UserID
			case tenant.RoleAdmin:
				admin = m.UserID
			}
		}
		if members[0].Role != tenant.RoleOwner || staff == uuid.Nil || admin == uuid.Nil {
			t.Fatalf("want owner first, then an admin and staff, got %+v", members)
		}
		if err := f.svc.RemoveMember(ctx, shopID, admin, tenant.RoleAdmin, ownerID); !errors.Is(err, team.ErrNotAllowed) {
			t.Fatalf("admin removing the owner: want ErrNotAllowed, got %v", err)
		}
		if err := f.svc.RemoveMember(ctx, shopID, ownerID, tenant.RoleOwner, ownerID); !errors.Is(err, team.ErrNotAllowed) {
			t.Fatalf("removing yourself: want ErrNotAllowed, got %v", err)
		}
		if err := f.svc.RemoveMember(ctx, shopID, admin, tenant.RoleAdmin, staff); err != nil {
			t.Fatalf("admin removing staff: %v", err)
		}
		if _, err := f.roleIn(t, shopID, staff); !errors.Is(err, tenant.ErrNotMember) {
			t.Fatalf("removed member: want ErrNotMember, got %v", err)
		}
		if err := f.svc.RemoveMember(ctx, shopID, ownerID, tenant.RoleOwner, staff); !errors.Is(err, team.ErrMemberNotFound) {
			t.Fatalf("already removed: want ErrMemberNotFound, got %v", err)
		}
	})
}
