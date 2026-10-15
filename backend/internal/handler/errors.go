package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// All API errors share one JSON shape, so clients can handle them in one place:
//
//	{"error": {"code": "email_taken", "message": "...", "details": [...]}}
type apiError struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Details []fieldError `json:"details,omitempty"`
}

type fieldError struct {
	Field string `json:"field"`
	Rule  string `json:"rule"`
}

func writeError(c *gin.Context, status int, code, message string, details ...fieldError) {
	c.AbortWithStatusJSON(status, gin.H{"error": apiError{Code: code, Message: message, Details: details}})
}

// writeInternal logs the real error but returns a generic message:
// internal details (SQL, stack traces) must never reach the client.
func writeInternal(c *gin.Context, err error) {
	log.Printf("internal error: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	writeError(c, http.StatusInternalServerError, "internal_error", "something went wrong")
}

// writeBindError turns a request-binding failure (bad JSON or failed `binding` rules) into a 400.
func writeBindError(c *gin.Context, err error) {
	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) {
		details := make([]fieldError, 0, len(verrs))
		for _, fe := range verrs {
			// Field() is the JSON name thanks to the tag-name func registered in server.New.
			details = append(details, fieldError{Field: fe.Field(), Rule: fe.Tag()})
		}
		writeError(c, http.StatusBadRequest, "validation_failed", "some fields are invalid", details...)
		return
	}
	writeError(c, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
}
