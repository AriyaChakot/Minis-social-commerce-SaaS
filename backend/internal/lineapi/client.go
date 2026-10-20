// Package lineapi is a small client for the LINE Messaging API: the calls the inbox makes to
// LINE (as opposed to webhooks, which LINE makes to us).
//
// Calling another company's API over the internet can be slow or fail, so every call has a
// timeout, transient failures (network errors, 5xx) are retried a few times with a pause, and
// sending a message carries a retry key so that a retry can never send it twice.
package lineapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultBaseURL is LINE's API. Tests point the client at a fake server instead.
const DefaultBaseURL = "https://api.line.me"

// MaxTextLength is LINE's limit for one text message (in characters).
const MaxTextLength = 5000

var (
	// ErrInvalidToken: LINE refused the channel access token (wrong, revoked or expired).
	ErrInvalidToken = errors.New("lineapi: channel access token rejected")
	// ErrRateLimited: too many requests, or the account's monthly message quota is used up.
	ErrRateLimited = errors.New("lineapi: rate limit or monthly quota reached")
	// ErrNotFound: e.g. the user blocked the account, so their profile is not available.
	ErrNotFound = errors.New("lineapi: not found")
)

// Error is any other answer from LINE that is not a success.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("lineapi: LINE answered %d: %s", e.Status, e.Message)
}

type Client struct {
	baseURL string
	http    *http.Client
	// Pauses between attempts; len(backoff)+1 attempts in total. Tests make them short.
	backoff []time.Duration
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		// One overall limit per attempt: connecting, waiting and reading the answer.
		http:    &http.Client{Timeout: 10 * time.Second},
		backoff: []time.Duration{300 * time.Millisecond, time.Second},
	}
}

// WithBackoff returns a copy that waits the given pauses between attempts (for tests).
func (c *Client) WithBackoff(pauses ...time.Duration) *Client {
	cp := *c
	cp.backoff = pauses
	return &cp
}

// Profile is a LINE user's public profile.
type Profile struct {
	DisplayName string `json:"displayName"`
	PictureURL  string `json:"pictureUrl"`
}

// Profile returns the profile of a user who added the account as a friend.
func (c *Client) Profile(ctx context.Context, token, userID string) (Profile, error) {
	var p Profile
	err := c.do(ctx, http.MethodGet, "/v2/bot/profile/"+url.PathEscape(userID), token, nil, "", &p)
	return p, err
}

// BotInfo describes the Official Account a token belongs to.
type BotInfo struct {
	BasicID     string `json:"basicId"` // "@123abcde": what customers search for
	DisplayName string `json:"displayName"`
}

// BotInfo is the cheapest call that proves a token works: used when the merchant saves one.
func (c *Client) BotInfo(ctx context.Context, token string) (BotInfo, error) {
	var b BotInfo
	err := c.do(ctx, http.MethodGet, "/v2/bot/info", token, nil, "", &b)
	return b, err
}

// PushText sends one text message to a user and returns LINE's id for it.
//
// retryKey (a UUID) makes the send idempotent on LINE's side: if an attempt reached LINE but its
// answer got lost, the retry is answered 409 "already accepted" instead of sending again. The
// caller passes the same key when the merchant retries the same message.
func (c *Client) PushText(ctx context.Context, token, to, text, retryKey string) (string, error) {
	body := map[string]any{
		"to":       to,
		"messages": []map[string]string{{"type": "text", "text": text}},
	}
	var res struct {
		SentMessages []struct {
			ID string `json:"id"`
		} `json:"sentMessages"`
	}
	err := c.do(ctx, http.MethodPost, "/v2/bot/message/push", token, body, retryKey, &res)
	if err != nil {
		return "", err
	}
	if len(res.SentMessages) == 0 {
		return "", nil // 409 without details: sent earlier, id unknown
	}
	return res.SentMessages[0].ID, nil
}

// do sends a request, retrying transient failures, and decodes a JSON answer into out.
func (c *Client) do(ctx context.Context, method, path, token string, body any, retryKey string, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("lineapi: encode request: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; ; attempt++ {
		retry, err := c.once(ctx, method, path, token, payload, retryKey, out)
		if err == nil || !retry || attempt >= len(c.backoff) {
			return err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last error: %v)", ctx.Err(), lastErr)
		case <-time.After(c.backoff[attempt]):
		}
	}
}

// once makes one attempt and reports whether a failure is worth retrying.
func (c *Client) once(ctx context.Context, method, path, token string, payload []byte, retryKey string, out any) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return false, fmt.Errorf("lineapi: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if retryKey != "" {
		req.Header.Set("X-Line-Retry-Key", retryKey)
	}

	res, err := c.http.Do(req)
	if err != nil {
		// Network error or timeout: retry, unless our own context is over.
		return ctx.Err() == nil, fmt.Errorf("lineapi: %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return true, fmt.Errorf("lineapi: read answer: %w", err)
	}

	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300,
		// "This retry key was already accepted": an earlier attempt did send the message.
		res.StatusCode == http.StatusConflict && retryKey != "":
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil && res.StatusCode != http.StatusConflict {
				return false, fmt.Errorf("lineapi: decode answer: %w", err)
			}
		}
		return false, nil
	case res.StatusCode == http.StatusUnauthorized:
		return false, ErrInvalidToken
	case res.StatusCode == http.StatusTooManyRequests:
		// Not retried right away: a quota will not come back in a second.
		return false, ErrRateLimited
	case res.StatusCode == http.StatusNotFound:
		return false, ErrNotFound
	default:
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		// 5xx: LINE's side had a problem; another attempt may work. 4xx: our request is wrong.
		return res.StatusCode >= 500, &Error{Status: res.StatusCode, Message: e.Message}
	}
}
