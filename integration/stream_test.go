//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// TestStreamText streams a chat reply, folds its events, and wants the Response completed with
// its usage, its text the deltas joined, and its message an id, the item_id of every delta.
func TestStreamText(t *testing.T) {
	c := chatCompletions(t)
	var acc protocol.Accumulator
	var deltas strings.Builder
	var events int
	itemIDs := make(map[string]bool)
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
			itemIDs[d.ItemID] = true
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
	for _, it := range resp.Output {
		if m, ok := it.(protocol.Message); ok && (m.ID == "" || len(itemIDs) != 1 || !itemIDs[m.ID]) {
			t.Errorf("message id %q, deltas' item ids %v; want an id, the item_id of every delta", m.ID, itemIDs)
		}
	}
	if u := resp.Usage; u.InputTokens == 0 || u.OutputTokens == 0 || u.TotalTokens != u.InputTokens+u.OutputTokens {
		t.Errorf("Usage = %+v, want input and output counted, and total their sum", u)
	}
	t.Logf("%d events, output_text %q, usage %+v", events, text, resp.Usage)
}
