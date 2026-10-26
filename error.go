package client

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
)

// maxErrorText bounds how much of an error body without a message Error prints. It bounds the
// text of the error, not what is read: Body holds the whole reply.
const maxErrorText = 512

// APIError is a non-2xx reply from the endpoint. Requests return it wrapped, so callers reach it
// with errors.As.
type APIError struct {
	// Status is the HTTP status code.
	Status int
	// Message is the endpoint's error message, when Body gave one: decoded by the Client's
	// Format for Generate, and as an OpenAI-style error body for Embed.
	Message string
	// Type is the endpoint's error type, such as "invalid_request_error", when it gave one.
	Type string
	// Code is the endpoint's error code, when it gave one. A numeric code is kept as its
	// decimal text.
	Code string
	// Body is the reply's body.
	Body []byte
}

// Error reports the status and the endpoint's message, with its type and code when it gave
// them, or the start of the body when it gave no message.
func (e *APIError) Error() string {
	s := fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status))
	if e.Message != "" {
		s += ": " + e.Message
		var detail []string
		for _, d := range []string{e.Type, e.Code} {
			if d != "" {
				detail = append(detail, d)
			}
		}
		if len(detail) > 0 {
			s += " (" + strings.Join(detail, ", ") + ")"
		}
		return s
	}
	body := bytes.TrimSpace(e.Body)
	switch {
	case len(body) == 0:
		return s
	case len(body) > maxErrorText:
		return fmt.Sprintf("%s: %s...", s, body[:maxErrorText])
	}
	return fmt.Sprintf("%s: %s", s, body)
}
