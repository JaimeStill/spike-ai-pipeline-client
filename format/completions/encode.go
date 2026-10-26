package completions

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// EncodeRequest encodes req, which the client has validated, as a Chat Completions request
// body. Instructions become a leading system message, and each message item a message of its
// role, its content a plain string when it is a single input_text or output_text part and an
// array of text and refusal parts otherwise. MaxOutputTokens goes out as max_completion_tokens.
// Native's members, the native tier, are merged into the top level, and a member named like one
// the typed fields set is refused. It adds no headers.
func (*Format) EncodeRequest(req *protocol.Request, _ http.Header) ([]byte, error) {
	body := request{
		Model:               req.Model,
		MaxCompletionTokens: req.MaxOutputTokens,
		Native:              req.Native,
	}
	if req.Instructions != "" {
		content, err := json.Marshal(req.Instructions)
		if err != nil {
			return nil, fmt.Errorf("completions: instructions: %w", err)
		}
		body.Messages = append(body.Messages, message{Role: string(protocol.RoleSystem), Content: content})
	}
	for i, it := range req.Input {
		m, err := encodeItem(it)
		if err != nil {
			return nil, fmt.Errorf("completions: input %d: %w", i, err)
		}
		body.Messages = append(body.Messages, m)
	}
	b, err := json.Marshal(body)
	if err != nil {
		var se *jsontext.SyntacticError
		if errors.As(err, &se) && errors.Is(se.Err, jsontext.ErrDuplicateName) {
			return nil, fmt.Errorf("completions: native member %s clashes with a member the request sets",
				se.JSONPointer.LastToken())
		}
		return nil, fmt.Errorf("completions: encode request: %w", err)
	}
	return b, nil
}

// encodeItem encodes one input item as a message.
func encodeItem(it protocol.Item) (message, error) {
	switch it := it.(type) {
	case protocol.Message:
		content, err := encodeContent(it.Content)
		if err != nil {
			return message{}, err
		}
		return message{Role: string(it.Role), Content: content}, nil
	default:
		return message{}, fmt.Errorf("item %T has no Chat Completions form", it)
	}
}

// encodeContent encodes a message's content: a single text part as a plain string, anything
// else as an array of parts.
func encodeContent(parts []protocol.ContentPart) (jsontext.Value, error) {
	if len(parts) == 1 {
		switch p := parts[0].(type) {
		case protocol.InputText:
			return json.Marshal(p.Text)
		case protocol.OutputText:
			return json.Marshal(p.Text)
		}
	}
	wire := make([]jsontext.Value, 0, len(parts))
	for i, p := range parts {
		var b jsontext.Value
		var err error
		switch p := p.(type) {
		case protocol.InputText:
			b, err = json.Marshal(textPart{Type: "text", Text: p.Text})
		case protocol.OutputText:
			b, err = json.Marshal(textPart{Type: "text", Text: p.Text})
		case protocol.Refusal:
			b, err = json.Marshal(refusalPart{Type: "refusal", Refusal: p.Refusal})
		default:
			return nil, fmt.Errorf("content %d: part %T has no Chat Completions form", i, p)
		}
		if err != nil {
			return nil, fmt.Errorf("content %d: %w", i, err)
		}
		wire = append(wire, b)
	}
	return json.Marshal(wire)
}
