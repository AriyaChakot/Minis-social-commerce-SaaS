package user_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mscs/internal/testutil/dbtest"
	"mscs/internal/user"
)

func TestNormalizeEmail(t *testing.T) {
	// Table-driven test: one loop, many cases.
	cases := []struct {
		in, want string
	}{
		{"a@example.com", "a@example.com"},
		{"  A@Example.COM  ", "a@example.com"},
		{"MiXeD.Case@Shop.co.th", "mixed.case@shop.co.th"},
	}
	for _, c := range cases {
		if got := user.NormalizeEmail(c.in); got != c.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRepository(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	repo := user.Repository{}
	email := "Repo-" + dbtest.Suffix() + "@Example.com"

	created, err := repo.Create(ctx, pool, "  "+email+"  ", "hash", "Repo User")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, created.ID)
	})

	t.Run("email is stored normalized", func(t *testing.T) {
		if created.Email != strings.ToLower(email) {
			t.Fatalf("want %q, got %q", strings.ToLower(email), created.Email)
		}
	})

	t.Run("same email in other case returns ErrEmailTaken", func(t *testing.T) {
		_, err := repo.Create(ctx, pool, strings.ToUpper(email), "hash", "Dup")
		if !errors.Is(err, user.ErrEmailTaken) {
			t.Fatalf("want ErrEmailTaken, got %v", err)
		}
	})

	t.Run("get by email ignores case", func(t *testing.T) {
		got, err := repo.GetByEmail(ctx, pool, strings.ToUpper(email))
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.ID != created.ID || got.PasswordHash != "hash" {
			t.Fatalf("unexpected user: %+v", got)
		}
	})

	t.Run("unknown email returns ErrNotFound", func(t *testing.T) {
		_, err := repo.GetByEmail(ctx, pool, "nobody-"+dbtest.Suffix()+"@example.com")
		if !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
