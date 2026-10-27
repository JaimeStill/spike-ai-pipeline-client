//go:build integration

package integration_test

import (
	"encoding/json/v2"
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

// TestStreamFunctionCall streams a reply that must call get_weather, folds its events, and wants
// the Response completed with one function call: a call id, the function's name, arguments that
// parse as a JSON object naming the city, the deltas joined, and an id, the item_id of every
// arguments delta.
func TestStreamFunctionCall(t *testing.T) {
	c := chatCompletions(t)
	var acc protocol.Accumulator
	var deltas strings.Builder
	var events int
	itemIDs := make(map[string]bool)
	for e, err := range c.GenerateStream(requestContext(t), &protocol.Request{
		Model:      chatModel,
		Input:      []protocol.Item{weatherQuestion},
		Tools:      []protocol.FunctionTool{getWeather},
		ToolChoice: protocol.ToolChoiceRequired,
	}) {
		if err != nil {
			t.Fatalf("GenerateStream: %v", err)
		}
		events++
		if d, ok := e.(protocol.FunctionCallArgumentsDelta); ok {
			deltas.WriteString(d.Delta)
			itemIDs[d.ItemID] = true
		}
		if err := acc.Add(e); err != nil {
			t.Fatalf("Accumulator.Add: %v", err)
		}
	}
	call := weatherCall(t, acc.Response())
	if call.Arguments != deltas.String() {
		t.Errorf("arguments %q, want the deltas joined, %q", call.Arguments, deltas.String())
	}
	var args struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil || args.City == "" {
		t.Errorf("arguments %q: %v; want a JSON object naming the city", call.Arguments, err)
	}
	if call.ID == "" || len(itemIDs) != 1 || !itemIDs[call.ID] {
		t.Errorf("function call id %q, arguments deltas' item ids %v; want an id, the item_id of every delta", call.ID, itemIDs)
	}
	t.Logf("%d events, function_call %s(%s), call_id %q, id %q", events, call.Name, call.Arguments, call.CallID, call.ID)
}
