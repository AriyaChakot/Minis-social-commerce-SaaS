package inbox

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Service implements the inbox use cases: receiving webhooks and the merchant's inbox.
type Service struct {
	pool     *pgxpool.Pool
	channels channelRepo
	store    *Store
	now      func() time.Time
}

func NewService(pool *pgxpool.Pool, store *Store) *Service {
	return &Service{pool: pool, store: store, now: time.Now}
}

// ReceiveLINE handles one webhook delivery for the channel in the URL and reports how many new
// messages it stored. The signature is checked against the raw body BEFORE it is parsed:
// nothing in an unsigned body is trusted, not even which shop it claims to be for.
func (s *Service) ReceiveLINE(ctx context.Context, channelID uuid.UUID, body []byte, signature string) (int, error) {
	ch, err := s.channels.byID(ctx, s.pool, channelID)
	if err != nil {
		return 0, err
	}
	if !VerifyLineSignature(ch.Secret, body, signature) {
		return 0, ErrBadSignature
	}
	msgs, err := parseLine(body)
	if err != nil {
		return 0, err
	}

	tenantID := ch.TenantID.String()
	now := s.now().UTC()
	stored := 0
	var errs []error
	for _, in := range msgs {
		conv, err := s.store.conversationFor(ctx, tenantID, ProviderLINE, in.ExternalUserID, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		added, err := s.store.addIncoming(ctx, conv, in, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("message %s: %w", in.ExternalID, err))
		}
		if added {
			stored++
		}
	}
	// Any error makes the webhook answer 500, so LINE delivers it again; the messages that did
	// get stored are then skipped as duplicates.
	return stored, errors.Join(errs...)
}

func (s *Service) Conversations(ctx context.Context, tenantID uuid.UUID) ([]Conversation, error) {
	return s.store.list(ctx, tenantID.String())
}

// Messages returns a conversation's latest messages. The conversation is looked up within the
// shop first, so an id from another shop is simply "not found".
func (s *Service) Messages(ctx context.Context, tenantID uuid.UUID, conversationID string) (Conversation, []Message, error) {
	id, err := bson.ObjectIDFromHex(conversationID)
	if err != nil {
		return Conversation{}, nil, ErrConversationNotFound
	}
	conv, err := s.store.get(ctx, tenantID.String(), id)
	if err != nil {
		return Conversation{}, nil, err
	}
	msgs, err := s.store.messagesOf(ctx, tenantID.String(), id)
	return conv, msgs, err
}

func (s *Service) MarkRead(ctx context.Context, tenantID uuid.UUID, conversationID string) error {
	id, err := bson.ObjectIDFromHex(conversationID)
	if err != nil {
		return ErrConversationNotFound
	}
	return s.store.markRead(ctx, tenantID.String(), id)
}

// LineChannel returns the shop's LINE connection, or ErrChannelNotFound if it has none.
func (s *Service) LineChannel(ctx context.Context, tenantID uuid.UUID) (LineChannel, error) {
	r, err := s.channels.byTenant(ctx, s.pool, tenantID)
	if err != nil {
		return LineChannel{}, err
	}
	return r.public(), nil
}

// Same format as the line_channels CHECK constraint.
var lineSecretPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ConnectLINE saves the channel secret from the LINE Developers console.
func (s *Service) ConnectLINE(ctx context.Context, tenantID uuid.UUID, secret string) (LineChannel, error) {
	secret = strings.ToLower(strings.TrimSpace(secret))
	if !lineSecretPattern.MatchString(secret) {
		return LineChannel{}, &ValidationError{"channel_secret", "must be the 32-character channel secret from the LINE Developers console"}
	}
	r, err := s.channels.upsert(ctx, s.pool, tenantID, secret)
	if err != nil {
		return LineChannel{}, err
	}
	return r.public(), nil
}
