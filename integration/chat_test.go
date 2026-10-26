//go:build integration

package integration_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
)

// userText is a user message of one input_text part.
func userText(s string) client.Message {
	return client.Message{Role: client.RoleUser, Content: []client.ContentPart{client.InputText{Text: s}}}
}

func TestChat(t *testing.T) {
	c := chatCompletions(t)
	resp, err := c.Respond(requestContext(t), &client.Request{
		Model:        chatModel,
		Instructions: "Answer in one word.",
		Input:        []client.Item{userText("What is the capital of France?")},
	})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if resp.Status != client.StatusCompleted {
		t.Fatalf("Status = %q (%+v), want completed", resp.Status, resp.IncompleteDetails)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("Output has %d items, want 1 message: %#v", len(resp.Output), resp.Output)
	}
	m, ok := resp.Output[0].(client.Message)
	if !ok || m.Role != client.RoleAssistant || m.Status != client.StatusCompleted {
		t.Fatalf("Output[0] = %#v, want a completed assistant message", resp.Output[0])
	}
	var text strings.Builder
	for _, p := range m.Content {
		if o, ok := p.(client.OutputText); ok {
			text.WriteString(o.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		t.Fatalf("the message has no output_text: %#v", m.Content)
	}
	if u := resp.Usage; u.InputTokens == 0 || u.OutputTokens == 0 || u.TotalTokens != u.InputTokens+u.OutputTokens {
		t.Errorf("Usage = %+v, want input and output counted, and total their sum", u)
	}
	t.Logf("output_text %q, usage %+v", text.String(), resp.Usage)
}

func TestChatIncompleteAtMaxOutputTokens(t *testing.T) {
	c := chatCompletions(t)
	const bound = 16
	resp, err := c.Respond(requestContext(t), &client.Request{
		Model:           chatModel,
		Input:           []client.Item{userText("Write a long essay on the history of Paris.")},
		MaxOutputTokens: bound,
	})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	want := client.IncompleteDetails{Reason: client.ReasonMaxOutputTokens}
	if resp.Status != client.StatusIncomplete || resp.IncompleteDetails == nil || *resp.IncompleteDetails != want {
		t.Errorf("Status = %q, IncompleteDetails = %+v; want incomplete, %+v", resp.Status, resp.IncompleteDetails, want)
	}
	if resp.Usage.OutputTokens > bound {
		t.Errorf("Usage.OutputTokens = %d, want at most %d", resp.Usage.OutputTokens, bound)
	}
}

func TestChatUnknownModelIsAPIError(t *testing.T) {
	c := chatCompletions(t)
	_, err := c.Respond(requestContext(t), &client.Request{
		Model: "spike-ai-pipeline/no-such-model",
		Input: []client.Item{userText("Hello.")},
	})
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Respond error = %v, want an *APIError", err)
	}
	if apiErr.Status != http.StatusBadRequest || apiErr.Message == "" {
		t.Errorf("APIError = %d %q, want 400 with the router's message", apiErr.Status, apiErr.Message)
	}
	t.Logf("APIError: %v", apiErr)
}
