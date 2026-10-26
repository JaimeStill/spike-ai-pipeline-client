package chatcompletions

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
)

// request is the Chat Completions request body.
type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	// MaxCompletionTokens carries max_output_tokens. llama.cpp b11669 reads it and max_tokens
	// alike, as aliases of n_predict; max_completion_tokens is the name OpenAI keeps, having
	// deprecated max_tokens.
	MaxCompletionTokens int `json:"max_completion_tokens,omitzero"`
	// Native is the request's native tier, its members merged into the top level. A member
	// named like one the fields above set is a duplicate name, which encoding refuses.
	Native jsontext.Value `json:",embed"`
}

// message is a Chat Completions message. Content is a string when the message is a single text
// part, and an array of parts otherwise.
type message struct {
	Role    string         `json:"role"`
	Content jsontext.Value `json:"content"`
}

// textPart is a Chat Completions text content part.
type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// refusalPart is a Chat Completions refusal content part.
type refusalPart struct {
	Type    string `json:"type"`
	Refusal string `json:"refusal"`
}

// EncodeRequest folds req, which the Client has validated, into a Chat Completions request
// body. Instructions become a leading system message, and each message item a message of its
// role, its content a plain string when it is a single input_text or output_text part and an
// array of text and refusal parts otherwise. MaxOutputTokens goes out as max_completion_tokens.
// Native's members, the native tier, are merged into the top level, and a member named like one
// the typed fields set is refused. It adds no headers.
func (*Format) EncodeRequest(req *client.Request, _ http.Header) ([]byte, error) {
	body := request{
		Model:               req.Model,
		MaxCompletionTokens: req.MaxOutputTokens,
		Native:              req.Native,
	}
	if req.Instructions != "" {
		content, err := json.Marshal(req.Instructions)
		if err != nil {
			return nil, fmt.Errorf("chatcompletions: instructions: %w", err)
		}
		body.Messages = append(body.Messages, message{Role: string(client.RoleSystem), Content: content})
	}
	for i, it := range req.Input {
		m, err := fold(it)
		if err != nil {
			return nil, fmt.Errorf("chatcompletions: input %d: %w", i, err)
		}
		body.Messages = append(body.Messages, m)
	}
	b, err := json.Marshal(body)
	if err != nil {
		var se *jsontext.SyntacticError
		if errors.As(err, &se) && errors.Is(se.Err, jsontext.ErrDuplicateName) {
			return nil, fmt.Errorf("chatcompletions: native member %s clashes with a member the request sets",
				se.JSONPointer.LastToken())
		}
		return nil, fmt.Errorf("chatcompletions: encode request: %w", err)
	}
	return b, nil
}

// fold folds one input item into a message.
func fold(it client.Item) (message, error) {
	switch it := it.(type) {
	case client.Message:
		content, err := foldContent(it.Content)
		if err != nil {
			return message{}, err
		}
		return message{Role: string(it.Role), Content: content}, nil
	default:
		return message{}, fmt.Errorf("item %T has no Chat Completions form", it)
	}
}

// foldContent encodes a message's content: a single text part as a plain string, anything
// else as an array of parts.
func foldContent(parts []client.ContentPart) (jsontext.Value, error) {
	if len(parts) == 1 {
		switch p := parts[0].(type) {
		case client.InputText:
			return json.Marshal(p.Text)
		case client.OutputText:
			return json.Marshal(p.Text)
		}
	}
	wire := make([]jsontext.Value, 0, len(parts))
	for i, p := range parts {
		var b jsontext.Value
		var err error
		switch p := p.(type) {
		case client.InputText:
			b, err = json.Marshal(textPart{Type: "text", Text: p.Text})
		case client.OutputText:
			b, err = json.Marshal(textPart{Type: "text", Text: p.Text})
		case client.Refusal:
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
