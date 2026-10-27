//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// TestReasoningRoundTrip asks with a reasoning effort for a call, wants reasoning ahead of it,
// sends the response's output back as it came, reasoning included, with the call's output, and
// wants a completed answer that uses it. That the reasoning reached the wire as the assistant
// message's reasoning_content is the unit tier's to prove; the exported API doesn't show the
// body.
func TestReasoningRoundTrip(t *testing.T) {
	c := chatCompletions(t)
	req := &protocol.Request{
		Model:      chatModel,
		Input:      []protocol.Item{weatherQuestion},
		Tools:      []protocol.FunctionTool{getWeather},
		ToolChoice: protocol.ToolChoiceRequired,
		Reasoning:  protocol.ReasoningParam{Effort: protocol.ReasoningEffortMedium},
	}
	resp, err := c.Generate(requestContext(t), req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	r, ok := resp.Output[0].(protocol.Reasoning)
	if !ok {
		t.Fatalf("Output[0] = %#v, want reasoning", resp.Output[0])
	}
	var reasoning strings.Builder
	for _, p := range r.Content {
		reasoning.WriteString(p.Text)
	}
	if strings.TrimSpace(reasoning.String()) == "" || r.EncryptedContent == "" || r.Status != protocol.StatusCompleted {
		t.Fatalf("reasoning = %#v, want it completed, with content and encrypted content", r)
	}
	call := weatherCall(t, resp)
	t.Logf("reasoning %q; reasoning tokens %d of %d output tokens",
		reasoning.String(), resp.Usage.OutputTokensDetails.ReasoningTokens, resp.Usage.OutputTokens)
	t.Logf("function_call %s(%s), call_id %q", call.Name, call.Arguments, call.CallID)

	req.Input = append(req.Input, resp.Output...)
	req.Input = append(req.Input, protocol.FunctionCallOutput{
		CallID: call.CallID,
		Output: `{"city":"Paris","celsius":17}`,
	})
	req.ToolChoice = protocol.ToolChoiceAuto
	resp, err = c.Generate(requestContext(t), req)
	if err != nil {
		t.Fatalf("Generate with the reasoning and the call's output: %v", err)
	}
	answer := completedText(t, resp)
	t.Logf("answer %q; reasoning tokens %d of %d output tokens",
		answer, resp.Usage.OutputTokensDetails.ReasoningTokens, resp.Usage.OutputTokens)
	if !strings.Contains(answer, "17") {
		t.Errorf("the answer %q doesn't use the tool's temperature, 17", answer)
	}
}
