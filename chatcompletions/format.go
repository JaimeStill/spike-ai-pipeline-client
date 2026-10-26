package chatcompletions

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// Format is the Chat Completions wire format. Its zero value is ready to use, and it is safe for
// concurrent use.
type Format struct{}

// New returns the Chat Completions format, for client.Config's Format.
func New() *Format {
	return &Format{}
}

// Route is "chat/completions", the Chat Completions endpoint below an OpenAI-compatible base
// URL such as "http://localhost:8080/v1".
func (*Format) Route() string {
	return "chat/completions"
}

// DecodeError reads an OpenAI-style error body, {"error": {"message", "type", "code"}}, where
// code is a string or a number. An error that is a bare string is the message. A body of any
// other shape gives nothing.
func (*Format) DecodeError(body []byte) (message, typ, code string) {
	var wire struct {
		Error jsontext.Value `json:"error"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return "", "", ""
	}
	switch wire.Error.Kind() {
	case jsontext.KindString:
		_ = json.Unmarshal(wire.Error, &message)
		return message, "", ""
	case jsontext.KindBeginObject:
		var e struct {
			Message string         `json:"message"`
			Type    string         `json:"type"`
			Code    jsontext.Value `json:"code"`
		}
		// A member of an unexpected type leaves the members decoded before it, which are still
		// the best account of the error there is.
		_ = json.Unmarshal(wire.Error, &e)
		switch e.Code.Kind() {
		case jsontext.KindString:
			_ = json.Unmarshal(e.Code, &code)
		case jsontext.KindNumber:
			code = string(e.Code)
		}
		return e.Message, e.Type, code
	}
	return "", "", ""
}
