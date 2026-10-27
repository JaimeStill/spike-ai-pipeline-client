//go:build integration

package integration_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"

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

// accumulate ranges a stream of req, folds its events, and returns the Response they make and
// the reasoning its reasoning deltas made, joined, failing t on a stream error or an event the
// Accumulator refuses.
func accumulate(t *testing.T, req *protocol.Request) (*protocol.Response, string) {
	t.Helper()
	var acc protocol.Accumulator
	var reasoning strings.Builder
	for e, err := range chatCompletions(t).GenerateStream(requestContext(t), req) {
		if err != nil {
			t.Fatalf("GenerateStream: %v", err)
		}
		if d, ok := e.(protocol.ReasoningDelta); ok {
			reasoning.WriteString(d.Delta)
		}
		if err := acc.Add(e); err != nil {
			t.Fatalf("Accumulator.Add: %v", err)
		}
	}
	return acc.Response(), reasoning.String()
}

// TestStreamReasoningRoundTrip streams a reply that must call get_weather, with a reasoning
// effort, and wants its accumulated output to hold reasoning ahead of the call: the reasoning
// deltas joined, and the encrypted content the format sends back. It sends that output back as
// it came, reasoning included, with the call's output, streams again, and wants the accumulated
// Response completed with an answer that uses it.
func TestStreamReasoningRoundTrip(t *testing.T) {
	req := &protocol.Request{
		Model:      chatModel,
		Input:      []protocol.Item{weatherQuestion},
		Tools:      []protocol.FunctionTool{getWeather},
		ToolChoice: protocol.ToolChoiceRequired,
		Reasoning:  protocol.ReasoningParam{Effort: protocol.ReasoningEffortMedium},
	}
	resp, deltas := accumulate(t, req)
	r, ok := resp.Output[0].(protocol.Reasoning)
	if !ok {
		t.Fatalf("Output[0] = %#v, want reasoning", resp.Output[0])
	}
	if strings.TrimSpace(deltas) == "" || len(r.Content) != 1 || r.Content[0].Text != deltas || r.EncryptedContent == "" || r.Status != protocol.StatusCompleted {
		t.Fatalf("reasoning = %#v, want it completed, with the reasoning deltas joined, %q, and encrypted content", r, deltas)
	}
	call := weatherCall(t, resp)
	t.Logf("reasoning %q; reasoning tokens %d of %d output tokens",
		deltas, resp.Usage.OutputTokensDetails.ReasoningTokens, resp.Usage.OutputTokens)
	t.Logf("function_call %s(%s), call_id %q", call.Name, call.Arguments, call.CallID)

	req.Input = append(req.Input, resp.Output...)
	req.Input = append(req.Input, protocol.FunctionCallOutput{
		CallID: call.CallID,
		Output: `{"city":"Paris","celsius":17}`,
	})
	req.ToolChoice = protocol.ToolChoiceAuto
	resp, _ = accumulate(t, req)
	answer := completedText(t, resp)
	t.Logf("answer %q; reasoning tokens %d of %d output tokens",
		answer, resp.Usage.OutputTokensDetails.ReasoningTokens, resp.Usage.OutputTokens)
	if !strings.Contains(answer, "17") {
		t.Errorf("the answer %q doesn't use the tool's temperature, 17", answer)
	}
}

// TestStreamCancel streams a long answer and cancels the context once a few output_text deltas
// have arrived. It wants no event after the cancel, the context's error as the sequence's one
// error, and the accumulated Response in progress, its message holding the text the deltas made
// so far. How long after the cancel the sequence ended is logged, not asserted.
func TestStreamCancel(t *testing.T) {
	const cancelAt = 5
	ctx, cancel := context.WithCancel(requestContext(t))
	defer cancel()
	var acc protocol.Accumulator
	var deltas strings.Builder
	var n int
	var errs []error
	var cancelled time.Time
	for e, err := range chatCompletions(t).GenerateStream(ctx, &protocol.Request{
		Model:     chatModel,
		Input:     []protocol.Item{userText("Write a long essay, of at least two thousand words, on the history of Paris.")},
		Reasoning: protocol.ReasoningParam{Effort: protocol.ReasoningEffortLow},
	}) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(errs) > 0 || ctx.Err() != nil {
			t.Errorf("event %#v follows the cancel or the error", e)
		}
		if err := acc.Add(e); err != nil {
			t.Fatalf("Accumulator.Add: %v", err)
		}
		if d, ok := e.(protocol.OutputTextDelta); ok {
			deltas.WriteString(d.Delta)
			if n++; n == cancelAt {
				cancel()
				cancelled = time.Now()
			}
		}
	}
	if cancelled.IsZero() {
		t.Fatalf("the stream ended after %d output_text deltas, before the cancel at %d; errors %v", n, cancelAt, errs)
	}
	ended := time.Since(cancelled)
	if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Fatalf("GenerateStream errors = %v, want one context.Canceled", errs)
	}
	resp := acc.Response()
	if resp.Status != protocol.StatusInProgress || len(resp.Output) == 0 {
		t.Fatalf("Response = %#v, want it in progress, with output", resp)
	}
	m, ok := resp.Output[len(resp.Output)-1].(protocol.Message)
	switch {
	case !ok:
		t.Fatalf("last output = %#v, want the message", resp.Output[len(resp.Output)-1])
	case m.Status != protocol.StatusInProgress || len(m.Content) != 1 || m.Content[0] != (protocol.OutputText{Text: deltas.String()}):
		t.Errorf("message = %#v, want it in progress, holding the deltas joined, %q", m, deltas.String())
	}
	t.Logf("cancelled after %d output_text deltas, %q; the sequence ended %v later with %v; %d output items",
		n, deltas.String(), ended, errs[0], len(resp.Output))
}
