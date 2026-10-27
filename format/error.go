package format

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
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
