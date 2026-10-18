package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"mscs/internal/inbox"
)

// InboxHandler exposes the chat inbox: the LINE webhook (called by LINE's servers) and the
// merchant's inbox under /api/v1/tenants/:tenantID.
type InboxHandler struct {
	svc *inbox.Service
}

func NewInboxHandler(svc *inbox.Service) *InboxHandler {
	return &InboxHandler{svc: svc}
}

// LINEWebhook handles POST /api/v1/webhooks/line/:channelID.
//
// LINE expects a quick 2xx. A 5xx makes LINE deliver the same events again (when redelivery is
// enabled), which is safe because storing a message is idempotent.
func (h *InboxHandler) LINEWebhook(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("channelID"))
	if err != nil {
		writeError(c, http.StatusNotFound, "channel_not_found", "unknown webhook")
		return
	}
	// The raw bytes, exactly as sent: the signature covers them, and re-encoding parsed JSON
	// would not give the same bytes back.
	body, err := c.GetRawData()
	if err != nil {
		writeError(c, http.StatusRequestEntityTooLarge, "body_too_large", "request body too large")
		return
	}

	stored, err := h.svc.ReceiveLINE(c.Request.Context(), channelID, body, c.GetHeader("X-Line-Signature"))
	switch {
	case err == nil:
		if stored > 0 {
			log.Printf("line webhook %s: stored %d new message(s)", channelID, stored)
		}
		c.Status(http.StatusOK)
	case errors.Is(err, inbox.ErrChannelNotFound):
		writeError(c, http.StatusNotFound, "channel_not_found", "unknown webhook")
	case errors.Is(err, inbox.ErrBadSignature):
		writeError(c, http.StatusUnauthorized, "bad_signature", "signature does not match")
	case errors.Is(err, inbox.ErrBadPayload):
		writeError(c, http.StatusBadRequest, "invalid_payload", "not a LINE webhook body")
	default:
		writeInternal(c, err)
	}
}

// Conversations handles GET /conversations: the most recent chats, newest activity first.
func (h *InboxHandler) Conversations(c *gin.Context) {
	t, _ := currentTenant(c)
	list, err := h.svc.Conversations(c.Request.Context(), t.ID)
	if err != nil {
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

// Messages handles GET /conversations/:conversationID/messages.
func (h *InboxHandler) Messages(c *gin.Context) {
	t, _ := currentTenant(c)
	conv, msgs, err := h.svc.Messages(c.Request.Context(), t.ID, c.Param("conversationID"))
	if errors.Is(err, inbox.ErrConversationNotFound) {
		writeError(c, http.StatusNotFound, "conversation_not_found", "conversation not found")
		return
	}
	if err != nil {
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversation": conv, "messages": msgs})
}

// MarkRead handles POST /conversations/:conversationID/read.
func (h *InboxHandler) MarkRead(c *gin.Context) {
	t, _ := currentTenant(c)
	err := h.svc.MarkRead(c.Request.Context(), t.ID, c.Param("conversationID"))
	if errors.Is(err, inbox.ErrConversationNotFound) {
		writeError(c, http.StatusNotFound, "conversation_not_found", "conversation not found")
		return
	}
	if err != nil {
		writeInternal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// LINEChannel handles GET /channels/line: {"line": null} when the shop has not connected LINE.
func (h *InboxHandler) LINEChannel(c *gin.Context) {
	t, _ := currentTenant(c)
	ch, err := h.svc.LineChannel(c.Request.Context(), t.ID)
	switch {
	case errors.Is(err, inbox.ErrChannelNotFound):
		c.JSON(http.StatusOK, gin.H{"line": nil})
	case err != nil:
		writeInternal(c, err)
	default:
		c.JSON(http.StatusOK, gin.H{"line": ch})
	}
}

type connectLINERequest struct {
	ChannelSecret string `json:"channel_secret" binding:"required,max=100"`
}

// ConnectLINE handles PUT /channels/line (owners and admins).
func (h *InboxHandler) ConnectLINE(c *gin.Context) {
	t, _ := currentTenant(c)
	var req connectLINERequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}
	ch, err := h.svc.ConnectLINE(c.Request.Context(), t.ID, req.ChannelSecret)
	var verr *inbox.ValidationError
	switch {
	case errors.As(err, &verr):
		writeError(c, http.StatusBadRequest, "validation_failed", verr.Message,
			fieldError{Field: verr.Field, Rule: "format"})
	case err != nil:
		writeInternal(c, err)
	default:
		c.JSON(http.StatusOK, gin.H{"line": ch})
	}
}
