package format

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"
)

// DecodeOpenAIError decodes an OpenAI-style error body, {"error": {"message", "type", "code"}},
// where code is a string or a number, and returns its message, type, and code. An error that is
// a bare string is the message. A body of any other shape gives nothing. It is the error body of
// every OpenAI-compatible endpoint, so the formats that target one use it for DecodeError, and
// the client uses it for its OpenAI-compatible operations outside the Format, such as
// embeddings.
func DecodeOpenAIError(body []byte) (message, typ, code string) {
	var wire openAIErrorBody
	if json.Unmarshal(body, &wire) != nil {
		return "", "", ""
	}
	switch wire.Error.Kind() {
	case jsontext.KindString:
		_ = json.Unmarshal(wire.Error, &message)
		return message, "", ""
	case jsontext.KindBeginObject:
		var e openAIErrorObject
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

// openAIErrorBody is an OpenAI-style error body. Error is an openAIErrorObject or, from some
// servers, a bare string.
type openAIErrorBody struct {
	Error jsontext.Value `json:"error"`
}

// openAIErrorObject is the object form of an error body's error. Code is a string or a number.
type openAIErrorObject struct {
	Message string         `json:"message"`
	Type    string         `json:"type"`
	Code    jsontext.Value `json:"code"`
}

// StreamError is an error frame in a streamed reply: the endpoint failed after the stream began
// and said so in the stream, as llama.cpp does with an event of data {"error": {...}}, in the
// shape of an OpenAI-style error body, before it ends the stream. A StreamDecoder returns it,
// wrapped, from Decode, and the client yields it as the stream's last pair, where callers reach
// it with errors.As, as client.StreamError.
type StreamError struct {
	// Message is the endpoint's error message.
	Message string
	// Type is the endpoint's error type, such as "server_error", when it gave one.
	Type string
	// Code is the endpoint's error code, when it gave one. A numeric code is kept as its
	// decimal text.
	Code string
}

// Error reports the endpoint's message, with its type and code when it gave them.
func (e *StreamError) Error() string {
	s := "stream error"
	if e.Message != "" {
		s += ": " + e.Message
	}
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
