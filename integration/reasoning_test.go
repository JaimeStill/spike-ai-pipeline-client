//go:build integration

package integration_test

import (
	"strings"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// TestReasoningRoundTrip asks with a reasoning effort for a call, wants reasoning ahead of it,
// sends the response's output back as it came, reasoning included, with the call's output, and
// wants a completed answer that uses it. That the reasoning reached the wire, as the assistant
// message's reasoning_content, its thinking block, or the reasoning item's content, is the unit
// tier's to prove; the exported API doesn't show the body.
func TestReasoningRoundTrip(t *testing.T) {
	eachFormat(t, func(t *testing.T, c *client.Client) {
		req := &protocol.Request{
			Model:           chatModel,
			Input:           []protocol.Item{weatherQuestion},
			Tools:           []protocol.FunctionTool{getWeather},
			ToolChoice:      protocol.ToolChoiceRequired,
			Reasoning:       protocol.ReasoningParam{Effort: protocol.ReasoningEffortMedium},
			MaxOutputTokens: chatBound,
		}
		resp, err := c.Generate(requestContext(t), req)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		r, ok := resp.Output[0].(protocol.Reasoning)
		if !ok {
			t.Fatalf("Output[0] = %#v, want reasoning", resp.Output[0])
		}
		reasoning := reasoningText(r)
		if strings.TrimSpace(reasoning) == "" || r.EncryptedContent == "" || r.Status != protocol.StatusCompleted {
			t.Fatalf("reasoning = %#v, want it completed, with content and encrypted content", r)
		}
		call := weatherCall(t, resp)
		t.Logf("reasoning %q; reasoning tokens %d of %d output tokens",
			reasoning, resp.Usage.OutputTokensDetails.ReasoningTokens, resp.Usage.OutputTokens)
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
	})
}

// TestReasoningEffort records what each reasoning effort does to gpt-oss-120b's reasoning, and
// passes either way: the router drops Messages' output_config, where the effort's level goes,
// and a model may reason as long at any effort.
func TestReasoningEffort(t *testing.T) {
	efforts := []protocol.ReasoningEffort{protocol.ReasoningEffortNone, protocol.ReasoningEffortLow, protocol.ReasoningEffortHigh}
	eachFormatOf(t, func(t *testing.T, c *client.Client, f testFormat) {
		for _, effort := range efforts {
			resp, err := c.Generate(requestContext(t), &protocol.Request{
				Model:           chatModel,
				Input:           []protocol.Item{userText("Is 391 a prime number? Answer yes or no.")},
				Reasoning:       protocol.ReasoningParam{Effort: effort},
				MaxOutputTokens: chatBound,
			})
			if err != nil {
				f.record(t, "effort %s: Generate error %v", effort, err)
				continue
			}
			var reasoning int
			for _, it := range resp.Output {
				if r, ok := it.(protocol.Reasoning); ok {
					reasoning += len(reasoningText(r))
				}
			}
			f.record(t, "effort %s: status %s, %d bytes of reasoning, %d output tokens",
				effort, resp.Status, reasoning, resp.Usage.OutputTokens)
		}
	})
}

// reasoningText returns r's reasoning text, its parts joined.
func reasoningText(r protocol.Reasoning) string {
	var b strings.Builder
	for _, p := range r.Content {
		b.WriteString(p.Text)
	}
	return b.String()
}
