package inbox_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/inbox"
	"mscs/internal/lineapi"
	"mscs/internal/secretbox"
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

// newBox returns a secretbox with a random key, like a fresh DATA_ENCRYPTION_KEY.
func newBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	rand.Read(key)
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func TestInbox(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	store := inbox.NewStore(dbtest.Mongo(t))
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	// No shop here has an access token, so LINE is never called: an unreachable address is fine.
	svc := inbox.NewService(pool, store, newBox(t), lineapi.New("http://127.0.0.1:1"))

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

const goodToken = "good-token"

// fakeLINE plays LINE's Messaging API: it knows one valid token and records the pushes.
type fakeLINE struct {
	mu       sync.Mutex
	pushes   []push
	profiles int
}

type push struct{ To, Text, RetryKey string }

func (f *fakeLINE) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+goodToken {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Authentication failed"}`))
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/v2/bot/info":
		w.Write([]byte(`{"basicId":"@mscs123","displayName":"MSCS Test Shop"}`))
	case strings.HasPrefix(r.URL.Path, "/v2/bot/profile/"):
		f.profiles++
		id := strings.TrimPrefix(r.URL.Path, "/v2/bot/profile/")
		fmt.Fprintf(w, `{"displayName":"คุณ %s","pictureUrl":"https://profile.example/%s"}`, id[len(id)-4:], id)
	case r.URL.Path == "/v2/bot/message/push":
		var body struct {
			To       string `json:"to"`
			Messages []struct{ Text string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.pushes = append(f.pushes, push{body.To, body.Messages[0].Text, r.Header.Get("X-Line-Retry-Key")})
		fmt.Fprintf(w, `{"sentMessages":[{"id":"%d"}]}`, 9000+len(f.pushes))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeLINE) sent() []push {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]push(nil), f.pushes...)
}

func (f *fakeLINE) profileCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.profiles
}

func TestReplies(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	store := inbox.NewStore(dbtest.Mongo(t))
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	line := &fakeLINE{}
	srv := httptest.NewServer(http.HandlerFunc(line.serve))
	t.Cleanup(srv.Close)
	svc := inbox.NewService(pool, store, newBox(t), lineapi.New(srv.URL).WithBackoff())

	shop := newShop(t, ctx, pool)
	ch, err := svc.ConnectLINE(ctx, shop, secret)
	if err != nil {
		t.Fatal(err)
	}
	receive := func(body []byte) {
		t.Helper()
		if _, err := svc.ReceiveLINE(ctx, ch.ID, body, inbox.SignLine(secret, body)); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("credentials are stored encrypted", func(t *testing.T) {
		var plain *string
		var enc []byte
		err := pool.QueryRow(ctx, `SELECT channel_secret, channel_secret_enc FROM line_channels WHERE tenant_id = $1`, shop).Scan(&plain, &enc)
		if err != nil {
			t.Fatal(err)
		}
		if plain != nil || len(enc) == 0 || bytes.Contains(enc, []byte(secret)) {
			t.Fatalf("want only ciphertext in the database, got plaintext %v, enc %x", plain, enc)
		}
	})

	t.Run("a message before the token stays nameless", func(t *testing.T) {
		receive(webhook(textEvent("Ucustomer0001", "m1", "สวัสดีค่ะ", 1760000000000)))
		list, _ := svc.Conversations(ctx, shop)
		if len(list) != 1 || list[0].DisplayName != "" {
			t.Fatalf("got %+v", list)
		}
	})

	t.Run("replying needs the access token", func(t *testing.T) {
		list, _ := svc.Conversations(ctx, shop)
		_, err := svc.Reply(ctx, shop, uuid.New(), list[0].ID.Hex(), inbox.ReplyInput{Text: "hi", ClientID: uuid.NewString()})
		if !errors.Is(err, inbox.ErrRepliesNotEnabled) {
			t.Fatalf("want ErrRepliesNotEnabled, got %v", err)
		}
	})

	t.Run("a token LINE refuses is not saved", func(t *testing.T) {
		var verr *inbox.ValidationError
		if _, err := svc.SetLineToken(ctx, shop, "wrong-token"); !errors.As(err, &verr) || verr.Field != "access_token" {
			t.Fatalf("want a validation error on access_token, got %v", err)
		}
		if got, _ := svc.LineChannel(ctx, shop); got.CanReply {
			t.Fatal("a refused token must not enable replies")
		}
		other := newShop(t, ctx, pool)
		if _, err := svc.SetLineToken(ctx, other, goodToken); !errors.As(err, &verr) || verr.Field != "channel_secret" {
			t.Fatalf("token before secret: want a validation error on channel_secret, got %v", err)
		}
	})

	t.Run("a valid token enables replies", func(t *testing.T) {
		got, err := svc.SetLineToken(ctx, shop, "  "+goodToken+"\n")
		if err != nil {
			t.Fatal(err)
		}
		if !got.CanReply || got.BotName != "MSCS Test Shop" || got.BotBasicID != "@mscs123" || got.ID != ch.ID {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("opening a nameless conversation fetches the name", func(t *testing.T) {
		list, _ := svc.Conversations(ctx, shop)
		conv, _, err := svc.Messages(ctx, shop, list[0].ID.Hex())
		if err != nil || conv.DisplayName != "คุณ 0001" || conv.PictureURL != "https://profile.example/Ucustomer0001" {
			t.Fatalf("got %q %q (err %v)", conv.DisplayName, conv.PictureURL, err)
		}
		// Saved: the next open does not ask LINE again.
		before := line.profileCalls()
		svc.Messages(ctx, shop, list[0].ID.Hex())
		if line.profileCalls() != before {
			t.Fatal("a known name must not be fetched again")
		}
	})

	t.Run("a new customer gets their name from the webhook", func(t *testing.T) {
		receive(webhook(textEvent("Ucustomer0002", "m2", "ขอราคาหน่อย", 1760000001000)))
		list, _ := svc.Conversations(ctx, shop)
		if list[0].DisplayName != "คุณ 0002" {
			t.Fatalf("got %q", list[0].DisplayName)
		}
	})

	t.Run("reply: sent through LINE once, stored, and shown as the preview", func(t *testing.T) {
		list, _ := svc.Conversations(ctx, shop)
		conv := list[0] // Ucustomer0002, 1 unread
		clientID := uuid.NewString()
		in := inbox.ReplyInput{Text: "  ราคา 290 บาทค่ะ  ", ClientID: clientID}

		msg, err := svc.Reply(ctx, shop, uuid.New(), conv.ID.Hex(), in)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Direction != inbox.DirectionOut || msg.Text != "ราคา 290 บาทค่ะ" {
			t.Fatalf("got %+v", msg)
		}
		sent := line.sent()
		if len(sent) != 1 || sent[0] != (push{"Ucustomer0002", "ราคา 290 บาทค่ะ", clientID}) {
			t.Fatalf("pushes: %+v", sent)
		}

		// Double submit (same client id): the same message back, nothing sent again.
		again, err := svc.Reply(ctx, shop, uuid.New(), conv.ID.Hex(), in)
		if err != nil || again.ID != msg.ID || len(line.sent()) != 1 {
			t.Fatalf("double submit: got %v (err %v), %d pushes", again.ID, err, len(line.sent()))
		}

		_, msgs, _ := svc.Messages(ctx, shop, conv.ID.Hex())
		if len(msgs) != 2 || msgs[1].ID != msg.ID {
			t.Fatalf("want the customer's message then the reply, got %d messages", len(msgs))
		}
		list, _ = svc.Conversations(ctx, shop)
		if list[0].LastMessageText != "ราคา 290 บาทค่ะ" || list[0].UnreadCount != 0 {
			t.Fatalf("conversation after reply: %+v", list[0])
		}
	})

	t.Run("invalid replies", func(t *testing.T) {
		list, _ := svc.Conversations(ctx, shop)
		id := list[0].ID.Hex()
		cases := []struct {
			field string
			in    inbox.ReplyInput
		}{
			{"text", inbox.ReplyInput{Text: "   ", ClientID: uuid.NewString()}},
			{"text", inbox.ReplyInput{Text: strings.Repeat("ก", lineapi.MaxTextLength+1), ClientID: uuid.NewString()}},
			{"client_id", inbox.ReplyInput{Text: "hi", ClientID: "not-a-uuid"}},
		}
		for _, tc := range cases {
			var verr *inbox.ValidationError
			if _, err := svc.Reply(ctx, shop, uuid.New(), id, tc.in); !errors.As(err, &verr) || verr.Field != tc.field {
				t.Fatalf("%s: want a validation error, got %v", tc.field, err)
			}
		}
		other := newShop(t, ctx, pool)
		if _, err := svc.Reply(ctx, other, uuid.New(), id, inbox.ReplyInput{Text: "hi", ClientID: uuid.NewString()}); !errors.Is(err, inbox.ErrConversationNotFound) {
			t.Fatalf("other shop: want not found, got %v", err)
		}
		if n := len(line.sent()); n != 1 {
			t.Fatalf("nothing more may be sent, got %d pushes", n)
		}
	})
}

// A secret saved in plaintext before migration 000012 is encrypted by EncryptLegacySecrets.
func TestEncryptLegacySecrets(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	svc := inbox.NewService(pool, inbox.NewStore(dbtest.Mongo(t)), newBox(t), lineapi.New("http://127.0.0.1:1"))

	// It encrypts EVERY plaintext row, with this test's throwaway key. Never let it touch another
	// shop's secret in the dev database: the dev API could no longer read it.
	var others int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM line_channels WHERE channel_secret IS NOT NULL`).Scan(&others); err != nil {
		t.Fatal(err)
	}
	if others > 0 {
		t.Skipf("%d plaintext secret(s) of other shops exist: start the API once to encrypt them", others)
	}

	shop := newShop(t, ctx, pool)
	var channelID uuid.UUID
	err := pool.QueryRow(ctx, `INSERT INTO line_channels (tenant_id, channel_secret) VALUES ($1, $2) RETURNING id`, shop, secret).Scan(&channelID)
	if err != nil {
		t.Fatal(err)
	}

	// Before: still readable, so webhooks keep working during the upgrade.
	body := webhook(textEvent("Ulegacy000001", "l1", "hi", time.Now().UnixMilli()))
	if _, err := svc.ReceiveLINE(ctx, channelID, body, inbox.SignLine(secret, body)); err != nil {
		t.Fatalf("before encryption: %v", err)
	}

	if n, err := svc.EncryptLegacySecrets(ctx); err != nil || n != 1 {
		t.Fatalf("want 1 encrypted, got %d (err %v)", n, err)
	}
	if n, err := svc.EncryptLegacySecrets(ctx); err != nil || n != 0 {
		t.Fatalf("second run: want 0, got %d (err %v)", n, err)
	}

	// After: no plaintext left, and the same secret still verifies webhooks.
	var plain *string
	if err := pool.QueryRow(ctx, `SELECT channel_secret FROM line_channels WHERE id = $1`, channelID).Scan(&plain); err != nil || plain != nil {
		t.Fatalf("plaintext must be cleared, got %v (err %v)", plain, err)
	}
	body = webhook(textEvent("Ulegacy000001", "l2", "hi again", time.Now().UnixMilli()))
	if _, err := svc.ReceiveLINE(ctx, channelID, body, inbox.SignLine(secret, body)); err != nil {
		t.Fatalf("after encryption: %v", err)
	}
}
