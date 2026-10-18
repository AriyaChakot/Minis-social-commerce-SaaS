package inbox

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"
)

// VerifyLineSignature checks the X-Line-Signature header: base64(HMAC-SHA256(channel secret, body)).
// Only LINE (and whoever has the secret) can produce it, so it proves where the webhook came from
// and that the body was not changed. It must be computed over the exact bytes received.
func VerifyLineSignature(secret string, body []byte, signature string) bool {
	got, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	// hmac.Equal takes the same time whether the first or the last byte differs, so an attacker
	// cannot find the right signature byte by byte by measuring response times.
	return hmac.Equal(got, mac.Sum(nil))
}

// SignLine computes the signature LINE would send (used by tests and the dev simulator).
func SignLine(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// lineWebhook is the body LINE posts. Events stay raw until we know their type.
type lineWebhook struct {
	Destination string            `json:"destination"`
	Events      []json.RawMessage `json:"events"`
}

// lineEvent holds the fields we use from a "message" event. Other event types (follow, unfollow,
// postback...) share the envelope; they are skipped for now.
type lineEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"` // milliseconds since 1970
	Source    struct {
		Type   string `json:"type"` // user, group, room
		UserID string `json:"userId"`
	} `json:"source"`
	Message struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"message"`
}

// incoming is a provider-neutral message ready to be stored.
type incoming struct {
	ExternalUserID string
	ExternalID     string
	Type           string
	Text           string
	SentAt         time.Time
	Raw            json.RawMessage
}

// parseLine extracts the one-to-one customer messages from a webhook body.
// The "Verify" button in the LINE console sends a body with no events: a valid, empty result.
func parseLine(body []byte) ([]incoming, error) {
	var hook lineWebhook
	if err := json.Unmarshal(body, &hook); err != nil {
		return nil, ErrBadPayload
	}
	var out []incoming
	for _, raw := range hook.Events {
		var ev lineEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, ErrBadPayload
		}
		// Group chats and non-message events are out of scope for this version.
		if ev.Type != "message" || ev.Source.Type != "user" || ev.Source.UserID == "" || ev.Message.ID == "" {
			continue
		}
		text := ""
		if ev.Message.Type == "text" {
			text = ev.Message.Text
		}
		out = append(out, incoming{
			ExternalUserID: ev.Source.UserID,
			ExternalID:     ev.Message.ID,
			Type:           ev.Message.Type,
			Text:           text,
			SentAt:         time.UnixMilli(ev.Timestamp).UTC(),
			Raw:            raw,
		})
	}
	return out, nil
}
