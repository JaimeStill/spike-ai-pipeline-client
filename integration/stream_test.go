//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// TestStreamText streams a chat reply, folds its events, and wants the Response completed with
// its usage, and its text the deltas joined.
func TestStreamText(t *testing.T) {
	c := chatCompletions(t)
	var acc protocol.Accumulator
	var deltas strings.Builder
	var events int
	for e, err := range c.GenerateStream(requestContext(t), &protocol.Request{
		Model:        chatModel,
		Instructions: "Answer in one sentence.",
		Input:        []protocol.Item{userText("What is the capital of France?")},
	}) {
		if err != nil {
			t.Fatalf("GenerateStream: %v", err)
		}
		events++
		if d, ok := e.(protocol.OutputTextDelta); ok {
			deltas.WriteString(d.Delta)
		}
		if err := acc.Add(e); err != nil {
			t.Fatalf("Accumulator.Add: %v", err)
		}
	}
	resp := acc.Response()
	text := completedText(t, resp)
	if text != deltas.String() {
		t.Errorf("output_text %q, want the deltas joined, %q", text, deltas.String())
	}
	if u := resp.Usage; u.InputTokens == 0 || u.OutputTokens == 0 || u.TotalTokens != u.InputTokens+u.OutputTokens {
		t.Errorf("Usage = %+v, want input and output counted, and total their sum", u)
	}
	t.Logf("%d events, output_text %q, usage %+v", events, text, resp.Usage)
}
