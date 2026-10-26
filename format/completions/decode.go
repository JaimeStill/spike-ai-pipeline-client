package completions

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// DecodeResponse decodes a Chat Completions response body. The first choice's message's
// reasoning_content, when it has one, becomes a reasoning item ahead of the others: its content
// one reasoning_text part of the text, and its encrypted_content the format's envelope holding
// it, which EncodeRequest reads back. The message then becomes an assistant message item, its content an output_text part and its refusal a refusal part;
// a message with neither gives no message item. Each of its tool_calls then becomes a
// function_call item, in order, its call_id the call's id; a call of a type other than function
// is an error. The first choice's finish_reason gives the response's status, which the items
// take:
//
//	stop, tool_calls  completed
//	length            incomplete, reason max_output_tokens
//	content_filter    incomplete, reason content_filter
//
// Any other finish_reason, or a body with no choices, is an error. The reply's usage gives the
// response's: prompt_tokens, completion_tokens, total_tokens, and
// completion_tokens_details.reasoning_tokens.
func (*Format) DecodeResponse(body []byte) (*protocol.Response, error) {
	var wire response
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("completions: decode response: %w", err)
	}
	if len(wire.Choices) == 0 {
		return nil, errors.New("completions: response has no choices")
	}
	choice := wire.Choices[0]
	resp := &protocol.Response{
		Usage: protocol.Usage{
			InputTokens:  wire.Usage.PromptTokens,
			OutputTokens: wire.Usage.CompletionTokens,
			TotalTokens:  wire.Usage.TotalTokens,
			OutputTokensDetails: protocol.OutputTokensDetails{
				ReasoningTokens: wire.Usage.CompletionTokensDetails.ReasoningTokens,
			},
		},
	}
	switch choice.FinishReason {
	case "stop", "tool_calls":
		resp.Status = protocol.StatusCompleted
	case "length":
		resp.Status = protocol.StatusIncomplete
		resp.IncompleteDetails = &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}
	case "content_filter":
		resp.Status = protocol.StatusIncomplete
		resp.IncompleteDetails = &protocol.IncompleteDetails{Reason: protocol.ReasonContentFilter}
	default:
		return nil, fmt.Errorf("completions: response has unknown finish_reason %q", choice.FinishReason)
	}
	if r := choice.Message.ReasoningContent; r != "" {
		sealed, err := sealEnvelope(envelope{ReasoningContent: r})
		if err != nil {
			return nil, fmt.Errorf("completions: reasoning: %w", err)
		}
		resp.Output = append(resp.Output, protocol.Reasoning{
			Content:          []protocol.ReasoningText{{Text: r}},
			EncryptedContent: sealed,
		})
	}
	var content []protocol.ContentPart
	if choice.Message.Content != "" {
		content = append(content, protocol.OutputText{Text: choice.Message.Content})
	}
	if choice.Message.Refusal != "" {
		content = append(content, protocol.Refusal{Refusal: choice.Message.Refusal})
	}
	if len(content) > 0 {
		resp.Output = append(resp.Output, protocol.Message{
			Status:  resp.Status,
			Role:    protocol.RoleAssistant,
			Content: content,
		})
	}
	for i, c := range choice.Message.ToolCalls {
		if c.Type != "function" {
			return nil, fmt.Errorf("completions: response tool call %d has unknown type %q", i, c.Type)
		}
		resp.Output = append(resp.Output, protocol.FunctionCall{
			Status:    resp.Status,
			CallID:    c.ID,
			Name:      c.Function.Name,
			Arguments: c.Function.Arguments,
		})
	}
	return resp, nil
}

// sealEnvelope returns e in its encrypted_content layout: envelopePrefix, then e as JSON in
// base64.
func sealEnvelope(e envelope) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return envelopePrefix + base64.StdEncoding.EncodeToString(b), nil
}

// DecodeError decodes an OpenAI-style error body, {"error": {"message", "type", "code"}}, where
// code is a string or a number. An error that is a bare string is the message. A body of any
// other shape gives nothing.
func (*Format) DecodeError(body []byte) (message, typ, code string) {
	var wire errorBody
	if json.Unmarshal(body, &wire) != nil {
		return "", "", ""
	}
	switch wire.Error.Kind() {
	case jsontext.KindString:
		_ = json.Unmarshal(wire.Error, &message)
		return message, "", ""
	case jsontext.KindBeginObject:
		var e errorObject
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
