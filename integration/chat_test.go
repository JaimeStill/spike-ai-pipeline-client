//go:build integration

package integration_test

import (
	"errors"
	"net/http"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// userText is a user message of one input_text part.
func userText(s string) protocol.Message {
	return protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.InputText{Text: s}}}
}

func TestChat(t *testing.T) {
	eachFormat(t, func(t *testing.T, c *client.Client) {
		resp, err := c.Generate(requestContext(t), &protocol.Request{
			Model:           chatModel,
			Instructions:    "Answer in one word.",
			Input:           []protocol.Item{userText("What is the capital of France?")},
			MaxOutputTokens: chatBound,
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		text := completedText(t, resp)
		if u := resp.Usage; u.InputTokens == 0 || u.OutputTokens == 0 || u.TotalTokens != u.InputTokens+u.OutputTokens {
			t.Errorf("Usage = %+v, want input and output counted, and total their sum", u)
		}
		t.Logf("output_text %q, usage %+v", text, resp.Usage)
	})
}

// TestChatIncompleteAtMaxOutputTokens asserts the response stops at the bound, incomplete; on a
// format the router reports completed there, it records the status instead.
func TestChatIncompleteAtMaxOutputTokens(t *testing.T) {
	eachFormatOf(t, func(t *testing.T, c *client.Client, f testFormat) {
		const bound = 16
		resp, err := c.Generate(requestContext(t), &protocol.Request{
			Model:           chatModel,
			Input:           []protocol.Item{userText("Write a long essay on the history of Paris.")},
			MaxOutputTokens: bound,
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		want := protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}
		if f.completesAtBound {
			f.record(t, "status %q, incomplete_details %+v, output_tokens %d at the bound %d",
				resp.Status, resp.IncompleteDetails, resp.Usage.OutputTokens, bound)
		} else if resp.Status != protocol.StatusIncomplete || resp.IncompleteDetails == nil || *resp.IncompleteDetails != want {
			t.Errorf("Status = %q, IncompleteDetails = %+v; want incomplete, %+v", resp.Status, resp.IncompleteDetails, want)
		}
		if resp.Usage.OutputTokens > bound {
			t.Errorf("Usage.OutputTokens = %d, want at most %d", resp.Usage.OutputTokens, bound)
		}
	})
}

func TestChatUnknownModelIsAPIError(t *testing.T) {
	eachFormat(t, func(t *testing.T, c *client.Client) {
		_, err := c.Generate(requestContext(t), &protocol.Request{
			Model:           "spike-ai-pipeline/no-such-model",
			Input:           []protocol.Item{userText("Hello.")},
			MaxOutputTokens: 16,
		})
		var apiErr *client.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("Generate error = %v, want an *APIError", err)
		}
		if apiErr.Status != http.StatusBadRequest || apiErr.Message == "" || apiErr.Type == "" {
			t.Errorf("APIError = %d %q (%q), want 400 with the router's message and type", apiErr.Status, apiErr.Message, apiErr.Type)
		}
		t.Logf("APIError: %v", apiErr)
	})
}
