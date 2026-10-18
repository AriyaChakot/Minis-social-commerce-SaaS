package inbox

import (
	"testing"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestVerifyLineSignature(t *testing.T) {
	body := []byte(`{"destination":"U1","events":[]}`)
	sig := SignLine(testSecret, body)

	cases := []struct {
		name   string
		secret string
		body   []byte
		sig    string
		want   bool
	}{
		{"valid", testSecret, body, sig, true},
		{"body changed by one byte", testSecret, []byte(`{"destination":"U2","events":[]}`), sig, false},
		{"other channel's secret", "ffffffffffffffffffffffffffffffff", body, sig, false},
		{"missing header", testSecret, body, "", false},
		{"not base64", testSecret, body, "%%%", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyLineSignature(tc.secret, tc.body, tc.sig); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestParseLine(t *testing.T) {
	t.Run("the console's Verify request has no events", func(t *testing.T) {
		msgs, err := parseLine([]byte(`{"destination":"U1","events":[]}`))
		if err != nil || len(msgs) != 0 {
			t.Fatalf("want no messages and no error, got %v %v", msgs, err)
		}
	})

	t.Run("keeps one-to-one messages, skips the rest", func(t *testing.T) {
		body := []byte(`{"destination":"U1","events":[
			{"type":"message","timestamp":1760000000000,"source":{"type":"user","userId":"Ua1"},
			 "message":{"id":"m1","type":"text","text":"สวัสดีค่ะ"}},
			{"type":"message","timestamp":1760000001000,"source":{"type":"user","userId":"Ua1"},
			 "message":{"id":"m2","type":"sticker","packageId":"446","stickerId":"1988"}},
			{"type":"message","timestamp":1760000002000,"source":{"type":"group","groupId":"G1","userId":"Ua1"},
			 "message":{"id":"m3","type":"text","text":"in a group"}},
			{"type":"follow","timestamp":1760000003000,"source":{"type":"user","userId":"Ua2"}}
		]}`)
		msgs, err := parseLine(body)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) != 2 {
			t.Fatalf("want 2 messages (text + sticker), got %d", len(msgs))
		}
		if msgs[0].Text != "สวัสดีค่ะ" || msgs[0].SentAt.UnixMilli() != 1760000000000 {
			t.Fatalf("text message: got %+v", msgs[0])
		}
		if msgs[1].Type != "sticker" || msgs[1].Text != "" || len(msgs[1].Raw) == 0 {
			t.Fatalf("sticker: want type sticker, no text, raw kept; got %+v", msgs[1])
		}
	})

	t.Run("not JSON", func(t *testing.T) {
		if _, err := parseLine([]byte(`hello`)); err != ErrBadPayload {
			t.Fatalf("want ErrBadPayload, got %v", err)
		}
	})
}

func TestPreviewKeepsThaiCharactersWhole(t *testing.T) {
	long := ""
	for range 100 {
		long += "ก"
	}
	p := preview(long)
	if r := []rune(p); len(r) != 81 || r[80] != '…' {
		t.Fatalf("want 80 characters and an ellipsis, got %d: %q", len(r), p)
	}
}
