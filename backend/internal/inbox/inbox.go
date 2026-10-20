// Package inbox collects the chat messages customers send to a shop on social networks (LINE for
// now), so the merchant can read every conversation in one place and turn a chat into an order.
//
// Storage is split by what each database is good at:
//   - PostgreSQL holds the channel connection (line_channels): small, relational, has a secret.
//   - MongoDB holds conversations and messages: append-heavy, and each provider's events have
//     their own shape (stickers, images, locations...), kept as-is in the message's raw field.
//
// MongoDB has no Row-Level Security: tenant isolation is the code's job here. Every Store
// method takes the tenant id and puts it first in every filter, and every index starts with it.
package inbox

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Provider names, as stored in conversations.channel and messages.channel.
const ProviderLINE = "line"

// Direction of a message, seen from the shop.
const (
	DirectionIn  = "in"  // customer -> shop
	DirectionOut = "out" // shop -> customer (replies from the inbox)
)

// Conversation is one customer's chat with one shop on one provider.
type Conversation struct {
	ID       bson.ObjectID `bson:"_id" json:"id"`
	TenantID string        `bson:"tenant_id" json:"-"`
	Channel  string        `bson:"channel" json:"channel"`
	// The provider's id for the customer (LINE userId). Not shown to the merchant.
	ExternalUserID string `bson:"external_user_id" json:"-"`
	// From the provider's profile; empty until it could be fetched (needs the access token).
	DisplayName string `bson:"display_name" json:"display_name"`
	PictureURL  string `bson:"picture_url,omitempty" json:"picture_url"`
	// Short, stable reference to tell nameless customers apart ("…a1b2c3").
	CustomerRef     string    `bson:"customer_ref" json:"customer_ref"`
	LastMessageAt   time.Time `bson:"last_message_at" json:"last_message_at"`
	LastMessageType string    `bson:"last_message_type" json:"last_message_type"`
	LastMessageText string    `bson:"last_message_text" json:"last_message_text"`
	UnreadCount     int       `bson:"unread_count" json:"unread_count"`
	CreatedAt       time.Time `bson:"created_at" json:"created_at"`
}

// Message is one chat message.
type Message struct {
	ID             bson.ObjectID `bson:"_id" json:"id"`
	TenantID       string        `bson:"tenant_id" json:"-"`
	ConversationID bson.ObjectID `bson:"conversation_id" json:"conversation_id"`
	Channel        string        `bson:"channel" json:"-"`
	// The provider's message id: the key that makes a redelivered webhook a no-op.
	ExternalID string    `bson:"external_id" json:"-"`
	Direction  string    `bson:"direction" json:"direction"`
	Type       string    `bson:"type" json:"type"` // text, sticker, image, video, audio, file, location
	Text       string    `bson:"text" json:"text"` // empty for non-text messages
	SentAt     time.Time `bson:"sent_at" json:"sent_at"`
	ReceivedAt time.Time `bson:"received_at" json:"-"`
	// SentBy: the user who replied (outgoing messages only), for the record.
	SentBy string `bson:"sent_by,omitempty" json:"-"`
	// The provider's event exactly as received. Nothing reads it yet; it keeps every detail
	// (sticker ids, image ids...) for later features without a schema change.
	Raw bson.Raw `bson:"raw,omitempty" json:"-"`
}

// LineChannel is a shop's LINE connection as the merchant sees it: never the secret itself.
type LineChannel struct {
	ID          uuid.UUID `json:"id"`
	WebhookPath string    `json:"webhook_path"`
	SecretHint  string    `json:"secret_hint"` // last 4 characters, to recognise which secret is set
	// CanReply: an access token is saved, so the inbox can reply and show customers' names.
	CanReply   bool      `json:"can_reply"`
	BotName    string    `json:"bot_name"`     // the Official Account the token belongs to
	BotBasicID string    `json:"bot_basic_id"` // e.g. "@123abcde"
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

var (
	ErrChannelNotFound      = errors.New("inbox: channel not found")
	ErrBadSignature         = errors.New("inbox: webhook signature does not match")
	ErrBadPayload           = errors.New("inbox: webhook body is not a valid LINE payload")
	ErrConversationNotFound = errors.New("inbox: conversation not found")

	// Replying needs the shop's channel access token.
	ErrRepliesNotEnabled = errors.New("inbox: replies need a LINE channel access token")
	// LINE refused the saved token (revoked or reissued): the merchant must save a new one.
	ErrLineTokenRejected = errors.New("inbox: LINE rejected the access token")
	// The account's monthly message quota is used up, or too many requests.
	ErrLineRateLimited = errors.New("inbox: LINE rate limit or monthly quota reached")
	// LINE could not be reached or failed; trying again later may work.
	ErrLineUnavailable = errors.New("inbox: LINE is unavailable")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("inbox: invalid %s: %s", e.Field, e.Message)
}
