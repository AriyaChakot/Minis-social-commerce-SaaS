package lineapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeLINE starts a server that answers with handler and counts the requests.
func fakeLINE(t *testing.T, handler http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization header: got %q", r.Header.Get("Authorization"))
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL).WithBackoff(time.Millisecond, time.Millisecond), &calls
}

func TestProfile(t *testing.T) {
	c, _ := fakeLINE(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/bot/profile/Uabc" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"userId":"Uabc","displayName":"น้องฝ้าย","pictureUrl":"https://profile.line-scdn.net/x","language":"th"}`))
	})
	p, err := c.Profile(context.Background(), "tok", "Uabc")
	if err != nil || p.DisplayName != "น้องฝ้าย" || p.PictureURL != "https://profile.line-scdn.net/x" {
		t.Fatalf("got %+v, %v", p, err)
	}
}

func TestPushText(t *testing.T) {
	c, calls := fakeLINE(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/bot/message/push" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Line-Retry-Key"); got != "key-1" {
			t.Errorf("retry key: got %q", got)
		}
		var body struct {
			To       string `json:"to"`
			Messages []struct{ Type, Text string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.To != "Uabc" || len(body.Messages) != 1 || body.Messages[0].Text != "ส่งพรุ่งนี้ค่ะ" {
			t.Errorf("body: %+v", body)
		}
		w.Write([]byte(`{"sentMessages":[{"id":"5001","quoteToken":"q"}]}`))
	})
	id, err := c.PushText(context.Background(), "tok", "Uabc", "ส่งพรุ่งนี้ค่ะ", "key-1")
	if err != nil || id != "5001" || calls.Load() != 1 {
		t.Fatalf("got id %q, err %v, %d calls", id, err, calls.Load())
	}
}

func TestPushRetriesServerErrorsWithTheSameKey(t *testing.T) {
	var attempts atomic.Int32
	c, calls := fakeLINE(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Line-Retry-Key") != "key-2" {
			t.Errorf("every attempt must carry the same retry key")
		}
		switch attempts.Add(1) {
		case 1:
			w.WriteHeader(http.StatusInternalServerError) // e.g. LINE sent it, then failed to answer
		default:
			// The retry: LINE recognises the key and does not send again.
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"message":"The retry key is already accepted","sentMessages":[{"id":"5002"}]}`))
		}
	})
	id, err := c.PushText(context.Background(), "tok", "Uabc", "hi", "key-2")
	if err != nil || id != "5002" || calls.Load() != 2 {
		t.Fatalf("got id %q, err %v, %d calls", id, err, calls.Load())
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		status    int
		want      error
		wantCalls int32
	}{
		{http.StatusUnauthorized, ErrInvalidToken, 1},
		{http.StatusTooManyRequests, ErrRateLimited, 1}, // a quota does not come back in a second
		{http.StatusNotFound, ErrNotFound, 1},
		{http.StatusBadRequest, nil, 1},         // our request is wrong: retrying will not help
		{http.StatusServiceUnavailable, nil, 3}, // LINE's problem: 1 attempt + 2 retries
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c, calls := fakeLINE(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(`{"message":"nope"}`))
			})
			_, err := c.BotInfo(context.Background(), "tok")
			var apiErr *Error
			switch {
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Fatalf("want %v, got %v", tc.want, err)
			case tc.want == nil && (!errors.As(err, &apiErr) || apiErr.Status != tc.status || apiErr.Message != "nope"):
				t.Fatalf("want *Error %d, got %v", tc.status, err)
			}
			if calls.Load() != tc.wantCalls {
				t.Fatalf("want %d calls, got %d", tc.wantCalls, calls.Load())
			}
		})
	}
}

func TestStopsWhenTheCallerGivesUp(t *testing.T) {
	c, _ := fakeLINE(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	c = c.WithBackoff(time.Hour) // would wait an hour before the retry...
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.BotInfo(ctx, "tok")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("want a prompt deadline error, got %v after %s", err, time.Since(start))
	}
}
