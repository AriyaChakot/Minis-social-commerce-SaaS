// Command linesim pretends to be LINE: it sends signed webhooks to the API, so the chat inbox
// can be tried without a LINE Official Account or a public URL (LINE cannot reach localhost).
//
//	.\scripts\go.ps1 --db run ./cmd/linesim -channel <id> -secret <secret>             # demo chat
//	.\scripts\go.ps1 --db run ./cmd/linesim -channel <id> -secret <secret> -text "สวัสดี" -user Ucustomer1
//
// The channel id is in the webhook URL shown on the shop's settings page; the secret is the one
// saved there. Default -api is the API's address inside the docker compose network.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"mscs/internal/inbox"
)

// demo is a short conversation from two customers (sticker = a LINE sticker message).
var demo = []struct{ user, text string }{
	{"U4af4980629a1b2c3d4e5f6a7b8c9d0e1", "สวัสดีค่ะ สนใจเสื้อผ้าฝ้ายสีครีมค่ะ"},
	{"U4af4980629a1b2c3d4e5f6a7b8c9d0e1", "มีไซส์ M ไหมคะ ส่งแถวลาดพร้าวได้ไหม"},
	{"U7c1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a", "ผ้าพันคอมัดย้อมเหลือกี่ผืนครับ"},
	{"U4af4980629a1b2c3d4e5f6a7b8c9d0e1", "sticker"},
	{"U7c1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a", "เอา 2 ผืนครับ โอนเงินแล้วส่งสลิปตรงนี้ได้เลยไหม"},
}

func main() {
	apiBase := flag.String("api", "http://api:8080", "API base URL")
	channel := flag.String("channel", "", "LINE channel id (from the webhook URL)")
	secret := flag.String("secret", "", "LINE channel secret saved for the shop")
	user := flag.String("user", "Ucustomer000000000000000000000001", "LINE userId of the sender (with -text)")
	text := flag.String("text", "", "send this one message instead of the demo chat")
	delay := flag.Duration("delay", 1500*time.Millisecond, "pause between demo messages")
	flag.Parse()
	if *channel == "" || *secret == "" {
		flag.Usage()
		log.Fatal("-channel and -secret are required")
	}

	url := *apiBase + "/api/v1/webhooks/line/" + *channel
	if *text != "" {
		send(url, *secret, *user, *text)
		return
	}
	for i, m := range demo {
		if i > 0 {
			time.Sleep(*delay)
		}
		send(url, *secret, m.user, m.text)
	}
}

// send posts one message event the way LINE does: JSON body + X-Line-Signature.
func send(url, secret, user, text string) {
	now := time.Now()
	message := map[string]any{"id": fmt.Sprintf("sim%d", now.UnixNano()), "type": "text", "text": text}
	if text == "sticker" {
		message = map[string]any{"id": fmt.Sprintf("sim%d", now.UnixNano()), "type": "sticker", "packageId": "446", "stickerId": "1988"}
	}
	body, _ := json.Marshal(map[string]any{
		"destination": "Usimulatedbot",
		"events": []any{map[string]any{
			"type":            "message",
			"mode":            "active",
			"timestamp":       now.UnixMilli(),
			"source":          map[string]any{"type": "user", "userId": user},
			"webhookEventId":  fmt.Sprintf("sim-%d", now.UnixNano()),
			"deliveryContext": map[string]any{"isRedelivery": false},
			"message":         message,
		}},
	})

	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Line-Signature", inbox.SignLine(secret, body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("send: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		log.Fatalf("API answered %d %s: %s", res.StatusCode, e.Error.Code, e.Error.Message)
	}
	fmt.Printf("sent %q from …%s\n", text, user[len(user)-6:])
}
