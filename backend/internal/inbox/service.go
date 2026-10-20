package inbox

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"mscs/internal/lineapi"
	"mscs/internal/secretbox"
)

// Service implements the inbox use cases: receiving webhooks, the merchant's inbox and replies.
type Service struct {
	pool     *pgxpool.Pool
	channels channelRepo
	store    *Store
	line     *lineapi.Client
	now      func() time.Time
}

// NewService wires the inbox. box encrypts the LINE credentials stored in PostgreSQL; line is
// the client for calls to LINE (replies, profiles).
func NewService(pool *pgxpool.Pool, store *Store, box *secretbox.Box, line *lineapi.Client) *Service {
	return &Service{pool: pool, channels: channelRepo{box: box}, store: store, line: line, now: time.Now}
}

// profileTimeout bounds the optional profile lookups, so a slow LINE cannot slow the webhook
// answer (LINE waits only a few seconds) or the inbox page much.
const profileTimeout = 3 * time.Second

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
	nameless := map[bson.ObjectID]Conversation{}
	for _, in := range msgs {
		conv, err := s.store.conversationFor(ctx, tenantID, ProviderLINE, in.ExternalUserID, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if conv.DisplayName == "" {
			nameless[conv.ID] = conv
		}
		added, err := s.store.addIncoming(ctx, conv, in, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("message %s: %w", in.ExternalID, err))
		}
		if added {
			stored++
		}
	}
	// Names are a nice-to-have: a failure is logged, never a reason to make LINE redeliver.
	for _, conv := range nameless {
		s.fetchProfile(ctx, ch, &conv)
	}
	// Any error makes the webhook answer 500, so LINE delivers it again; the messages that did
	// get stored are then skipped as duplicates.
	return stored, errors.Join(errs...)
}

// fetchProfile asks LINE for the customer's name and picture and saves them on the conversation.
// Best effort: without a token, or if LINE does not answer in time, the conversation stays nameless.
func (s *Service) fetchProfile(ctx context.Context, ch lineChannelRow, conv *Conversation) {
	if ch.Token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, profileTimeout)
	defer cancel()
	p, err := s.line.Profile(ctx, ch.Token, conv.ExternalUserID)
	if err == nil && p.DisplayName != "" {
		err = s.store.setProfile(ctx, conv.TenantID, conv.ID, p.DisplayName, p.PictureURL)
	}
	if err != nil {
		log.Printf("inbox: profile of conversation %s: %v", conv.ID.Hex(), err)
		return
	}
	conv.DisplayName, conv.PictureURL = p.DisplayName, p.PictureURL
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
	// Conversations from before the token was saved get their name when they are opened.
	if conv.DisplayName == "" {
		if ch, err := s.channels.byTenant(ctx, s.pool, tenantID); err == nil {
			s.fetchProfile(ctx, ch, &conv)
		}
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

// ReplyInput is a reply typed by the merchant. ClientID is a UUID the browser makes once per
// message: sending the same message twice (double click, retry after a timeout) sends it once.
type ReplyInput struct {
	Text     string
	ClientID string
}

// Reply sends a text message to the customer of a conversation through LINE, and stores it.
//
// The order matters: LINE first, then our database. If saving fails after LINE accepted the
// message, the browser's retry (same ClientID, so same retry key) is answered "already accepted"
// by LINE, and the save is done then. The customer never gets the message twice.
func (s *Service) Reply(ctx context.Context, tenantID, userID uuid.UUID, conversationID string, in ReplyInput) (Message, error) {
	text := strings.TrimSpace(in.Text)
	switch {
	case text == "":
		return Message{}, &ValidationError{"text", "must not be empty"}
	case utf8.RuneCountInString(text) > lineapi.MaxTextLength:
		return Message{}, &ValidationError{"text", fmt.Sprintf("must be at most %d characters", lineapi.MaxTextLength)}
	}
	clientID, err := uuid.Parse(in.ClientID)
	if err != nil {
		return Message{}, &ValidationError{"client_id", "must be a UUID"}
	}

	id, err := bson.ObjectIDFromHex(conversationID)
	if err != nil {
		return Message{}, ErrConversationNotFound
	}
	conv, err := s.store.get(ctx, tenantID.String(), id)
	if err != nil {
		return Message{}, err
	}

	// Already sent and saved (a double submit): answer with the first one, send nothing.
	externalID := "out:" + clientID.String()
	existing, err := s.store.messageByExternalID(ctx, conv.TenantID, conv.Channel, externalID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return Message{}, err
	}

	ch, err := s.channels.byTenant(ctx, s.pool, tenantID)
	if errors.Is(err, ErrChannelNotFound) || (err == nil && ch.Token == "") {
		return Message{}, ErrRepliesNotEnabled
	}
	if err != nil {
		return Message{}, err
	}

	lineID, err := s.line.PushText(ctx, ch.Token, conv.ExternalUserID, text, clientID.String())
	if err != nil {
		return Message{}, lineError(err)
	}

	raw, err := bson.Marshal(bson.D{{Key: "line_message_id", Value: lineID}})
	if err != nil {
		return Message{}, fmt.Errorf("inbox: encode reply: %w", err)
	}
	now := s.now().UTC()
	msg, _, err := s.store.addOutgoing(ctx, conv, Message{
		ID:             bson.NewObjectID(),
		TenantID:       conv.TenantID,
		ConversationID: conv.ID,
		Channel:        conv.Channel,
		ExternalID:     externalID,
		Direction:      DirectionOut,
		Type:           "text",
		Text:           text,
		SentAt:         now,
		ReceivedAt:     now,
		SentBy:         userID.String(),
		Raw:            raw,
	})
	return msg, err
}

// lineError turns a LINE API failure into the inbox error the merchant can act on.
func lineError(err error) error {
	switch {
	case errors.Is(err, lineapi.ErrInvalidToken):
		return ErrLineTokenRejected
	case errors.Is(err, lineapi.ErrRateLimited):
		return ErrLineRateLimited
	default:
		return fmt.Errorf("%w: %v", ErrLineUnavailable, err)
	}
}

// LineChannel returns the shop's LINE connection, or ErrChannelNotFound if it has none.
func (s *Service) LineChannel(ctx context.Context, tenantID uuid.UUID) (LineChannel, error) {
	r, err := s.channels.byTenant(ctx, s.pool, tenantID)
	if err != nil {
		return LineChannel{}, err
	}
	return r.public(), nil
}

// Same format as the line_channels CHECK constraint of migration 000011.
var lineSecretPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ConnectLINE saves the channel secret from the LINE Developers console.
func (s *Service) ConnectLINE(ctx context.Context, tenantID uuid.UUID, secret string) (LineChannel, error) {
	secret = strings.ToLower(strings.TrimSpace(secret))
	if !lineSecretPattern.MatchString(secret) {
		return LineChannel{}, &ValidationError{"channel_secret", "must be the 32-character channel secret from the LINE Developers console"}
	}
	r, err := s.channels.upsertSecret(ctx, s.pool, tenantID, secret)
	if err != nil {
		return LineChannel{}, err
	}
	return r.public(), nil
}

// SetLineToken saves the channel access token, after checking with LINE that it works: a typo
// is caught now, not when the merchant first tries to reply to a customer.
func (s *Service) SetLineToken(ctx context.Context, tenantID uuid.UUID, token string) (LineChannel, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 1000 || strings.ContainsAny(token, " \t\r\n") {
		return LineChannel{}, &ValidationError{"access_token", "must be the channel access token from the LINE Developers console"}
	}
	if _, err := s.channels.byTenant(ctx, s.pool, tenantID); errors.Is(err, ErrChannelNotFound) {
		return LineChannel{}, &ValidationError{"channel_secret", "save the channel secret first"}
	} else if err != nil {
		return LineChannel{}, err
	}

	bot, err := s.line.BotInfo(ctx, token)
	if errors.Is(err, lineapi.ErrInvalidToken) {
		return LineChannel{}, &ValidationError{"access_token", "LINE did not accept this token"}
	}
	if err != nil {
		return LineChannel{}, lineError(err)
	}

	r, err := s.channels.setToken(ctx, s.pool, tenantID, token, bot.DisplayName, bot.BasicID)
	if err != nil {
		return LineChannel{}, err
	}
	return r.public(), nil
}

// EncryptLegacySecrets encrypts the channel secrets stored in plaintext before migration 000012
// (the "migrate" step of expand-migrate-contract). The API runs it at startup; it is a no-op
// once done.
func (s *Service) EncryptLegacySecrets(ctx context.Context) (int, error) {
	return s.channels.encryptLegacy(ctx, s.pool)
}
