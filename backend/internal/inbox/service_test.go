package inbox_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/inbox"
	"mscs/internal/tenant"
	"mscs/internal/testutil/dbtest"
)

const secret = "0123456789abcdef0123456789abcdef"

func newShop(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	shop, err := tenant.Repository{}.Create(ctx, pool, "inbox-test-"+dbtest.Suffix(), "Inbox Test")
	if err != nil {
		t.Fatalf("create shop: %v", err)
	}
	// line_channels rows go with the tenant (ON DELETE CASCADE).
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, shop.ID) })
	return shop.ID
}

// textEvent builds one LINE text-message event; ms is the send time in milliseconds.
func textEvent(user, msgID, text string, ms int64) string {
	return fmt.Sprintf(`{"type":"message","timestamp":%d,"source":{"type":"user","userId":%q},`+
		`"message":{"id":%q,"type":"text","text":%q}}`, ms, user, msgID, text)
}

func webhook(events ...string) []byte {
	body := `{"destination":"Ubot","events":[`
	for i, e := range events {
		if i > 0 {
			body += ","
		}
		body += e
	}
	return []byte(body + `]}`)
}

func TestInbox(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	store := inbox.NewStore(dbtest.Mongo(t))
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	svc := inbox.NewService(pool, store)

	shop := newShop(t, ctx, pool)
	other := newShop(t, ctx, pool)

	if _, err := svc.ConnectLINE(ctx, shop, "not-a-secret"); err == nil {
		t.Fatal("a malformed secret must be refused")
	}
	ch, err := svc.ConnectLINE(ctx, shop, secret)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	receive := func(body []byte) (int, error) {
		return svc.ReceiveLINE(ctx, ch.ID, body, inbox.SignLine(secret, body))
	}

	t.Run("unsigned, wrongly signed or unknown webhooks are refused", func(t *testing.T) {
		body := webhook(textEvent("Uattacker", "x1", "fake", 1))
		if _, err := svc.ReceiveLINE(ctx, ch.ID, body, ""); !errors.Is(err, inbox.ErrBadSignature) {
			t.Fatalf("no signature: want ErrBadSignature, got %v", err)
		}
		if _, err := svc.ReceiveLINE(ctx, ch.ID, body, inbox.SignLine("ffffffffffffffffffffffffffffffff", body)); !errors.Is(err, inbox.ErrBadSignature) {
			t.Fatalf("wrong secret: want ErrBadSignature, got %v", err)
		}
		if _, err := svc.ReceiveLINE(ctx, uuid.New(), body, inbox.SignLine(secret, body)); !errors.Is(err, inbox.ErrChannelNotFound) {
			t.Fatalf("unknown channel: want ErrChannelNotFound, got %v", err)
		}
		if list, _ := svc.Conversations(ctx, shop); len(list) != 0 {
			t.Fatalf("nothing may be stored from refused webhooks, got %d conversations", len(list))
		}
	})

	t.Run("the console's Verify request is accepted", func(t *testing.T) {
		if n, err := receive(webhook()); err != nil || n != 0 {
			t.Fatalf("want 0 stored and no error, got %d %v", n, err)
		}
	})

	// Two customers: A sends two texts, B one sticker.
	sticker := `{"type":"message","timestamp":1760000005000,"source":{"type":"user","userId":"Ubbbbbb222222"},` +
		`"message":{"id":"b1","type":"sticker","packageId":"446","stickerId":"1988"}}`
	first := webhook(
		textEvent("Uaaaaaa111111", "a1", "สนใจเสื้อสีครีมค่ะ", 1760000000000),
		textEvent("Uaaaaaa111111", "a2", "มีไซส์ M ไหมคะ", 1760000002000),
		sticker,
	)

	t.Run("messages are grouped into one conversation per customer", func(t *testing.T) {
		if n, err := receive(first); err != nil || n != 3 {
			t.Fatalf("want 3 stored, got %d (err %v)", n, err)
		}
		list, err := svc.Conversations(ctx, shop)
		if err != nil || len(list) != 2 {
			t.Fatalf("want 2 conversations, got %d (err %v)", len(list), err)
		}
		// Newest activity first: B (sticker at :05) before A (last text at :02).
		b, a := list[0], list[1]
		if b.LastMessageType != "sticker" || b.UnreadCount != 1 || b.CustomerRef != "222222" {
			t.Fatalf("customer B: got %+v", b)
		}
		if a.LastMessageText != "มีไซส์ M ไหมคะ" || a.UnreadCount != 2 {
			t.Fatalf("customer A: got %+v", a)
		}
	})

	conversationOf := func(t *testing.T, ref string) inbox.Conversation {
		t.Helper()
		list, _ := svc.Conversations(ctx, shop)
		for _, c := range list {
			if c.CustomerRef == ref {
				return c
			}
		}
		t.Fatalf("no conversation for %s", ref)
		return inbox.Conversation{}
	}

	t.Run("a redelivered webhook changes nothing", func(t *testing.T) {
		if n, err := receive(first); err != nil || n != 0 {
			t.Fatalf("want 0 new, got %d (err %v)", n, err)
		}
		if a := conversationOf(t, "111111"); a.UnreadCount != 2 {
			t.Fatalf("unread must stay 2, got %d", a.UnreadCount)
		}
	})

	t.Run("a late, older message does not become the preview", func(t *testing.T) {
		if _, err := receive(webhook(textEvent("Uaaaaaa111111", "a0", "ข้อความเก่า", 1760000001000))); err != nil {
			t.Fatal(err)
		}
		a := conversationOf(t, "111111")
		if a.LastMessageText != "มีไซส์ M ไหมคะ" || a.LastMessageAt.UnixMilli() != 1760000002000 || a.UnreadCount != 3 {
			t.Fatalf("got preview %q at %d, unread %d", a.LastMessageText, a.LastMessageAt.UnixMilli(), a.UnreadCount)
		}

		_, msgs, err := svc.Messages(ctx, shop, a.ID.Hex())
		if err != nil || len(msgs) != 3 {
			t.Fatalf("want 3 messages, got %d (err %v)", len(msgs), err)
		}
		if msgs[0].Text != "สนใจเสื้อสีครีมค่ะ" || msgs[1].Text != "ข้อความเก่า" || msgs[2].Text != "มีไซส์ M ไหมคะ" {
			t.Fatalf("messages must be in send order, got %q %q %q", msgs[0].Text, msgs[1].Text, msgs[2].Text)
		}
	})

	t.Run("mark read", func(t *testing.T) {
		a := conversationOf(t, "111111")
		if err := svc.MarkRead(ctx, shop, a.ID.Hex()); err != nil {
			t.Fatal(err)
		}
		if a := conversationOf(t, "111111"); a.UnreadCount != 0 {
			t.Fatalf("want 0 unread, got %d", a.UnreadCount)
		}
	})

	t.Run("another shop cannot see or touch these conversations", func(t *testing.T) {
		a := conversationOf(t, "111111")
		if list, err := svc.Conversations(ctx, other); err != nil || len(list) != 0 {
			t.Fatalf("other shop's list: want empty, got %d (err %v)", len(list), err)
		}
		if _, _, err := svc.Messages(ctx, other, a.ID.Hex()); !errors.Is(err, inbox.ErrConversationNotFound) {
			t.Fatalf("other shop reading messages: want not found, got %v", err)
		}
		if err := svc.MarkRead(ctx, other, a.ID.Hex()); !errors.Is(err, inbox.ErrConversationNotFound) {
			t.Fatalf("other shop marking read: want not found, got %v", err)
		}
		if _, _, err := svc.Messages(ctx, shop, "not-an-id"); !errors.Is(err, inbox.ErrConversationNotFound) {
			t.Fatalf("malformed id: want not found, got %v", err)
		}
	})

	t.Run("replacing the secret keeps the webhook URL", func(t *testing.T) {
		again, err := svc.ConnectLINE(ctx, shop, "ABCDEF0123456789abcdef0123456789")
		if err != nil {
			t.Fatal(err)
		}
		if again.ID != ch.ID || again.WebhookPath != ch.WebhookPath || again.SecretHint != "…6789" {
			t.Fatalf("got %+v, before %+v", again, ch)
		}
		// The old secret no longer verifies.
		body := webhook(textEvent("Uaaaaaa111111", "a9", "x", 1760000009000))
		if _, err := svc.ReceiveLINE(ctx, ch.ID, body, inbox.SignLine(secret, body)); !errors.Is(err, inbox.ErrBadSignature) {
			t.Fatalf("old secret: want ErrBadSignature, got %v", err)
		}
	})
}
