package responses

import (
	"encoding/json/v2"
	"fmt"

	"github.com/JaimeStill/spike-ai-pipeline-client/format"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// DecodeResponse decodes a response resource into a response, its output items in the server's
// order:
//
//	message        a message of its role: an output_text part for each output_text, and a
//	               refusal part for each refusal, their annotations and logprobs dropped
//	reasoning      a reasoning item: its content's reasoning_text parts, its summary's
//	               summary_text parts, and its encrypted_content as the server gave it
//	function_call  a function_call item: its call_id, name, and arguments
//
// An output item of any other type is an error naming it, as is a part of a type its item
// doesn't take. Every item's id and status are the server's, as it gave them: the format derives
// nothing.
//
// The response's status is the server's too, and must be completed, incomplete, or failed: one
// in progress or queued is a background response, which a stateless request never asks for, and
// is an error, as is any other. incomplete_details' reason, such as max_output_tokens or
// content_filter, and a failed response's error, its code and message, are taken as given. The
// usage is the server's input_tokens, output_tokens, total_tokens, and
// output_tokens_details.reasoning_tokens.
func (*Format) DecodeResponse(body []byte) (*protocol.Response, error) {
	var wire response
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("responses: decode response: %w", err)
	}
	resp := &protocol.Response{Usage: protocol.Usage{
		InputTokens:  wire.Usage.InputTokens,
		OutputTokens: wire.Usage.OutputTokens,
		TotalTokens:  wire.Usage.TotalTokens,
		OutputTokensDetails: protocol.OutputTokensDetails{
			ReasoningTokens: wire.Usage.OutputTokensDetails.ReasoningTokens,
		},
	}}
	switch s := protocol.Status(wire.Status); s {
	case protocol.StatusCompleted, protocol.StatusIncomplete, protocol.StatusFailed:
		resp.Status = s
	default:
		return nil, fmt.Errorf("responses: response has status %q, not completed, incomplete, or failed", wire.Status)
	}
	if d := wire.IncompleteDetails; d != nil {
		resp.IncompleteDetails = &protocol.IncompleteDetails{Reason: protocol.IncompleteReason(d.Reason)}
	}
	if e := wire.Error; e != nil {
		resp.Error = &protocol.ResponseError{Code: e.Code, Message: e.Message}
	}
	for i, w := range wire.Output {
		it, err := decodeItem(w)
		if err != nil {
			return nil, fmt.Errorf("responses: output item %d: %w", i, err)
		}
		resp.Output = append(resp.Output, it)
	}
	return resp, nil
}

// decodeItem returns the output item of w, as DecodeResponse describes.
func decodeItem(w outputItem) (protocol.Item, error) {
	status := protocol.Status(w.Status)
	switch w.Type {
	case "message":
		m := protocol.Message{ID: w.ID, Status: status, Role: protocol.Role(w.Role)}
		for i, p := range w.Content {
			switch p.Type {
			case "output_text":
				m.Content = append(m.Content, protocol.OutputText{Text: p.Text})
			case "refusal":
				m.Content = append(m.Content, protocol.Refusal{Refusal: p.Refusal})
			default:
				return nil, fmt.Errorf("message content %d has unknown type %q", i, p.Type)
			}
		}
		return m, nil
	case "reasoning":
		r := protocol.Reasoning{ID: w.ID, Status: status, EncryptedContent: w.EncryptedContent}
		for i, p := range w.Content {
			if p.Type != "reasoning_text" {
				return nil, fmt.Errorf("reasoning content %d has unknown type %q", i, p.Type)
			}
			r.Content = append(r.Content, protocol.ReasoningText{Text: p.Text})
		}
		for i, p := range w.Summary {
			if p.Type != "summary_text" {
				return nil, fmt.Errorf("reasoning summary %d has unknown type %q", i, p.Type)
			}
			r.Summary = append(r.Summary, protocol.SummaryText{Text: p.Text})
		}
		return r, nil
	case "function_call":
		return protocol.FunctionCall{
			ID:        w.ID,
			Status:    status,
			CallID:    w.CallID,
			Name:      w.Name,
			Arguments: w.Arguments,
		}, nil
	}
	return nil, fmt.Errorf("unknown type %q", w.Type)
}

// DecodeError decodes the error body of a non-2xx reply. The spec defines none, so the format
// reads the OpenAI-style one its endpoints answer with, llama.cpp's router among them,
// {"error": {"message", "type", "code"}}, through format.DecodeOpenAIError, which gives a
// numeric code, as the router's is, as its decimal text.
func (*Format) DecodeError(body []byte) (message, typ, code string) {
	return format.DecodeOpenAIError(body)
}
