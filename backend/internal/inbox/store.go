package inbox

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Store keeps conversations and messages in MongoDB. Every method is scoped to one tenant:
// there is no RLS in MongoDB, so the tenant filter is written here, once, for every query.
type Store struct {
	conversations *mongo.Collection
	messages      *mongo.Collection
}

func NewStore(db *mongo.Database) *Store {
	return &Store{conversations: db.Collection("conversations"), messages: db.Collection("messages")}
}

// EnsureIndexes creates the indexes (a no-op when they exist), at startup. Every index starts
// with tenant_id: queries always filter on it, and it keeps each shop's data together.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	_, err := s.conversations.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// One conversation per customer per provider per shop; also what the upsert matches on.
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "channel", Value: 1}, {Key: "external_user_id", Value: 1}},
			Options: options.Index().SetUnique(true)},
		// The inbox list: newest activity first.
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "last_message_at", Value: -1}}},
	})
	if err != nil {
		return fmt.Errorf("inbox: conversation indexes: %w", err)
	}
	_, err = s.messages.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// Idempotency: LINE may deliver the same webhook twice; the second insert is rejected.
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "channel", Value: 1}, {Key: "external_id", Value: 1}},
			Options: options.Index().SetUnique(true)},
		// A conversation's messages in time order.
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "conversation_id", Value: 1}, {Key: "sent_at", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("inbox: message indexes: %w", err)
	}
	return nil
}

// conversationFor returns the customer's conversation, creating it on their first message.
// The upsert is atomic: two first messages arriving together still make one conversation
// (the unique index guarantees it, and MongoDB retries the losing upsert as an update).
func (s *Store) conversationFor(ctx context.Context, tenantID, channel, externalUserID string, now time.Time) (Conversation, error) {
	filter := bson.D{{Key: "tenant_id", Value: tenantID}, {Key: "channel", Value: channel}, {Key: "external_user_id", Value: externalUserID}}
	update := bson.D{{Key: "$setOnInsert", Value: bson.D{
		{Key: "display_name", Value: ""},
		{Key: "customer_ref", Value: customerRef(externalUserID)},
		{Key: "last_message_at", Value: time.Time{}},
		{Key: "last_message_type", Value: ""},
		{Key: "last_message_text", Value: ""},
		{Key: "unread_count", Value: 0},
		{Key: "created_at", Value: now},
	}}}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var c Conversation
	if err := s.conversations.FindOneAndUpdate(ctx, filter, update, opts).Decode(&c); err != nil {
		return Conversation{}, fmt.Errorf("inbox: upsert conversation: %w", err)
	}
	return c, nil
}

// addIncoming stores a customer message and updates its conversation. It reports false when the
// message was already stored (a redelivered webhook): nothing changes then.
//
// MongoDB transactions need a replica set, which this setup does not run, so the steps are
// ordered to be safe without one. The message insert comes first and is idempotent; the counters
// are updated only after a NEW message. If the process dies in between, a redelivery finds the
// message already stored and the unread counter misses one: it may undercount, never double count.
func (s *Store) addIncoming(ctx context.Context, conv Conversation, in incoming, now time.Time) (bool, error) {
	var raw bson.Raw
	if len(in.Raw) > 0 {
		if err := bson.UnmarshalExtJSON(in.Raw, false, &raw); err != nil {
			return false, fmt.Errorf("inbox: convert raw event: %w", err)
		}
	}
	msg := Message{
		ID:             bson.NewObjectID(),
		TenantID:       conv.TenantID,
		ConversationID: conv.ID,
		Channel:        conv.Channel,
		ExternalID:     in.ExternalID,
		Direction:      DirectionIn,
		Type:           in.Type,
		Text:           in.Text,
		SentAt:         in.SentAt,
		ReceivedAt:     now,
		Raw:            raw,
	}
	if _, err := s.messages.InsertOne(ctx, msg); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return false, nil
		}
		return false, fmt.Errorf("inbox: insert message: %w", err)
	}

	byID := bson.D{{Key: "tenant_id", Value: conv.TenantID}, {Key: "_id", Value: conv.ID}}
	// $max: webhooks can arrive out of order; an older message must not move the clock back.
	_, err := s.conversations.UpdateOne(ctx, byID, bson.D{
		{Key: "$inc", Value: bson.D{{Key: "unread_count", Value: 1}}},
		{Key: "$max", Value: bson.D{{Key: "last_message_at", Value: in.SentAt}}},
	})
	if err != nil {
		return true, fmt.Errorf("inbox: update conversation: %w", err)
	}
	// The preview follows the newest message only: this matches nothing if a newer one is in.
	_, err = s.conversations.UpdateOne(ctx,
		append(byID, bson.E{Key: "last_message_at", Value: in.SentAt}),
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "last_message_type", Value: in.Type},
			{Key: "last_message_text", Value: preview(in.Text)},
		}}},
	)
	if err != nil {
		return true, fmt.Errorf("inbox: update preview: %w", err)
	}
	return true, nil
}

// addOutgoing stores a reply the shop sent. msg.ExternalID is derived from the client's id for
// the message, so a double submit finds the first one: it is returned with false, unchanged.
// A reply also means the merchant has read the conversation.
func (s *Store) addOutgoing(ctx context.Context, conv Conversation, msg Message) (Message, bool, error) {
	if _, err := s.messages.InsertOne(ctx, msg); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			existing, err := s.messageByExternalID(ctx, conv.TenantID, conv.Channel, msg.ExternalID)
			return existing, false, err
		}
		return Message{}, false, fmt.Errorf("inbox: insert reply: %w", err)
	}

	byID := bson.D{{Key: "tenant_id", Value: conv.TenantID}, {Key: "_id", Value: conv.ID}}
	_, err := s.conversations.UpdateOne(ctx, byID, bson.D{
		{Key: "$set", Value: bson.D{{Key: "unread_count", Value: 0}}},
		{Key: "$max", Value: bson.D{{Key: "last_message_at", Value: msg.SentAt}}},
	})
	if err != nil {
		return msg, true, fmt.Errorf("inbox: update conversation: %w", err)
	}
	_, err = s.conversations.UpdateOne(ctx,
		append(byID, bson.E{Key: "last_message_at", Value: msg.SentAt}),
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "last_message_type", Value: msg.Type},
			{Key: "last_message_text", Value: preview(msg.Text)},
		}}},
	)
	if err != nil {
		return msg, true, fmt.Errorf("inbox: update preview: %w", err)
	}
	return msg, true, nil
}

func (s *Store) messageByExternalID(ctx context.Context, tenantID, channel, externalID string) (Message, error) {
	var m Message
	err := s.messages.FindOne(ctx, bson.D{
		{Key: "tenant_id", Value: tenantID}, {Key: "channel", Value: channel}, {Key: "external_id", Value: externalID},
	}).Decode(&m)
	if err != nil {
		return Message{}, fmt.Errorf("inbox: find message: %w", err)
	}
	return m, nil
}

// setProfile saves the customer's name and picture from the provider.
func (s *Store) setProfile(ctx context.Context, tenantID string, id bson.ObjectID, name, pictureURL string) error {
	_, err := s.conversations.UpdateOne(ctx,
		bson.D{{Key: "tenant_id", Value: tenantID}, {Key: "_id", Value: id}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "display_name", Value: name}, {Key: "picture_url", Value: pictureURL}}}},
	)
	if err != nil {
		return fmt.Errorf("inbox: set profile: %w", err)
	}
	return nil
}

// ConversationLimit: the inbox shows the most recent conversations only (MVP).
const ConversationLimit = 50

func (s *Store) list(ctx context.Context, tenantID string) ([]Conversation, error) {
	cur, err := s.conversations.Find(ctx,
		bson.D{{Key: "tenant_id", Value: tenantID}},
		options.Find().SetSort(bson.D{{Key: "last_message_at", Value: -1}}).SetLimit(ConversationLimit),
	)
	if err != nil {
		return nil, fmt.Errorf("inbox: list conversations: %w", err)
	}
	out := []Conversation{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("inbox: decode conversations: %w", err)
	}
	return out, nil
}

func (s *Store) get(ctx context.Context, tenantID string, id bson.ObjectID) (Conversation, error) {
	var c Conversation
	err := s.conversations.FindOne(ctx, bson.D{{Key: "tenant_id", Value: tenantID}, {Key: "_id", Value: id}}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Conversation{}, ErrConversationNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("inbox: get conversation: %w", err)
	}
	return c, nil
}

// MessageLimit: a conversation shows its latest messages only (MVP).
const MessageLimit = 200

// messagesOf returns the newest MessageLimit messages of a conversation, oldest first.
func (s *Store) messagesOf(ctx context.Context, tenantID string, conversationID bson.ObjectID) ([]Message, error) {
	cur, err := s.messages.Find(ctx,
		bson.D{{Key: "tenant_id", Value: tenantID}, {Key: "conversation_id", Value: conversationID}},
		// Newest first so the limit keeps the latest ones; reversed below for reading order.
		options.Find().SetSort(bson.D{{Key: "sent_at", Value: -1}}).SetLimit(MessageLimit),
	)
	if err != nil {
		return nil, fmt.Errorf("inbox: list messages: %w", err)
	}
	out := []Message{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("inbox: decode messages: %w", err)
	}
	slices.Reverse(out)
	return out, nil
}

func (s *Store) markRead(ctx context.Context, tenantID string, id bson.ObjectID) error {
	res, err := s.conversations.UpdateOne(ctx,
		bson.D{{Key: "tenant_id", Value: tenantID}, {Key: "_id", Value: id}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "unread_count", Value: 0}}}},
	)
	if err != nil {
		return fmt.Errorf("inbox: mark read: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrConversationNotFound
	}
	return nil
}

// customerRef is the last 6 characters of the provider's user id.
func customerRef(externalUserID string) string {
	if len(externalUserID) <= 6 {
		return externalUserID
	}
	return externalUserID[len(externalUserID)-6:]
}

// preview shortens a message for the inbox list, without cutting a Thai character in half.
func preview(text string) string {
	const maxRunes = 80
	r := []rune(text)
	if len(r) <= maxRunes {
		return text
	}
	return string(r[:maxRunes]) + "…"
}
