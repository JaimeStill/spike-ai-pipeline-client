package chatcompletions

import (
	"encoding/json/v2"
	"errors"
	"fmt"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
)

// response is the part of a Chat Completions response body the format reads. Members it
// doesn't read, such as reasoning_content and llama.cpp's timings, are ignored.
type response struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens            int `json:"prompt_tokens"`
		CompletionTokens        int `json:"completion_tokens"`
		TotalTokens             int `json:"total_tokens"`
		CompletionTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

// DecodeResponse unfolds a Chat Completions response body. The first choice's message becomes
// an assistant message item, its content an output_text part and its refusal a refusal part;
// a message with neither gives no item. The first choice's finish_reason gives the response's
// status, which the message item takes:
//
//	stop, tool_calls  completed
//	length            incomplete, reason max_output_tokens
//	content_filter    incomplete, reason content_filter
//
// Any other finish_reason, or a body with no choices, is an error. The reply's usage gives the
// response's: prompt_tokens, completion_tokens, total_tokens, and
// completion_tokens_details.reasoning_tokens.
func (*Format) DecodeResponse(body []byte) (*client.Response, error) {
	var wire response
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("chatcompletions: decode response: %w", err)
	}
	if len(wire.Choices) == 0 {
		return nil, errors.New("chatcompletions: response has no choices")
	}
	choice := wire.Choices[0]
	resp := &client.Response{
		Usage: client.Usage{
			InputTokens:  wire.Usage.PromptTokens,
			OutputTokens: wire.Usage.CompletionTokens,
			TotalTokens:  wire.Usage.TotalTokens,
			OutputTokensDetails: client.OutputTokensDetails{
				ReasoningTokens: wire.Usage.CompletionTokensDetails.ReasoningTokens,
			},
		},
	}
	switch choice.FinishReason {
	case "stop", "tool_calls":
		resp.Status = client.StatusCompleted
	case "length":
		resp.Status = client.StatusIncomplete
		resp.IncompleteDetails = &client.IncompleteDetails{Reason: client.ReasonMaxOutputTokens}
	case "content_filter":
		resp.Status = client.StatusIncomplete
		resp.IncompleteDetails = &client.IncompleteDetails{Reason: client.ReasonContentFilter}
	default:
		return nil, fmt.Errorf("chatcompletions: response has unknown finish_reason %q", choice.FinishReason)
	}
	var content []client.ContentPart
	if choice.Message.Content != "" {
		content = append(content, client.OutputText{Text: choice.Message.Content})
	}
	if choice.Message.Refusal != "" {
		content = append(content, client.Refusal{Refusal: choice.Message.Refusal})
	}
	if len(content) > 0 {
		resp.Output = append(resp.Output, client.Message{
			Status:  resp.Status,
			Role:    client.RoleAssistant,
			Content: content,
		})
	}
	return resp, nil
}
