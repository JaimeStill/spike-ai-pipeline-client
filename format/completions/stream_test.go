package completions_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/format/completions"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/sse"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// streamResult is what a stream's decoder made: its events, and the error that ended it, nil
// when it ended with its terminal event.
type streamResult struct {
	events []protocol.Event
	err    error
}

// decodeStream feeds body, a text/event-stream reply, to a new stream decoder, event by event as
// the client does, and returns the events it made, telling it the stream ended at the body's
// end. It stops at the first error.
func decodeStream(t *testing.T, body string) streamResult {
	t.Helper()
	decoder := completions.New().NewStreamDecoder()
	events := sse.NewDecoder(strings.NewReader(body))
	var r streamResult
	for {
		e, err := events.Next()
		if errors.Is(err, io.EOF) {
			made, err := decoder.End()
			r.events = append(r.events, made...)
			r.err = err
			return r
		}
		if err != nil {
			t.Fatalf("sse: %v", err)
		}
		made, err := decoder.Decode([]byte(e.Data))
		r.events = append(r.events, made...)
		if err != nil {
			r.err = err
			return r
		}
	}
}

// stream is an event stream of data events, one per chunk, as the router writes them.
func stream(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		fmt.Fprintf(&b, "data: %s\n\n", c)
	}
	return b.String()
}

// delta is a chunk of reply chatcmpl-1 of one choice with delta and finish_reason, null when
// finish is empty.
func delta(delta, finish string) string {
	reason := "null"
	if finish != "" {
		reason = `"` + finish + `"`
	}
	return `{"id":"chatcmpl-1","choices":[{"index":0,"delta":` + delta + `,"finish_reason":` + reason + `}],"object":"chat.completion.chunk"}`
}

// usageChunk is reply chatcmpl-1's chunk of no choices that carries the usage.
const usageChunk = `{"id":"chatcmpl-1","choices":[],"usage":{"completion_tokens":5,"prompt_tokens":7,"total_tokens":12}}`

// readTestdata returns the file name in testdata.
func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// trace renders events one per line, each its spec type and the members it carries: a part
// event's output index, content index, and item id, then its part or text, and an arguments
// event's output index and item id, then its arguments text. A run of reasoning deltas, which a
// recorded stream makes by the dozen, is one line of its first and last sequence numbers and its
// count; the reasoning.done after it gives their text joined, which the Accumulator checks. Each
// backtick is written \x60, so a trace fits in a raw string.
func trace(events []protocol.Event) string {
	var b strings.Builder
	for i := 0; i < len(events); i++ {
		switch e := events[i].(type) {
		case protocol.ReasoningDelta:
			n := 1
			for i+n < len(events) {
				if _, ok := events[i+n].(protocol.ReasoningDelta); !ok {
					break
				}
				n++
			}
			if n == 1 {
				fmt.Fprintf(&b, "%d response.reasoning.delta %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Delta)
				continue
			}
			last := events[i+n-1].(protocol.ReasoningDelta)
			fmt.Fprintf(&b, "%d-%d response.reasoning.delta %d %d %s, %d deltas\n", e.SequenceNumber, last.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, n)
			i += n - 1
		case protocol.ReasoningDone:
			fmt.Fprintf(&b, "%d response.reasoning.done %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Text)
		case protocol.ResponseCreated:
			fmt.Fprintf(&b, "%d response.created %s\n", e.SequenceNumber, e.Response.Status)
		case protocol.ResponseCompleted:
			fmt.Fprintf(&b, "%d response.completed %d items\n", e.SequenceNumber, len(e.Response.Output))
		case protocol.ResponseIncomplete:
			fmt.Fprintf(&b, "%d response.incomplete %d items\n", e.SequenceNumber, len(e.Response.Output))
		case protocol.ResponseFailed:
			fmt.Fprintf(&b, "%d response.failed %d items\n", e.SequenceNumber, len(e.Response.Output))
		case protocol.ErrorEvent:
			fmt.Fprintf(&b, "%d error %#v\n", e.SequenceNumber, e.Error)
		case protocol.OutputItemAdded:
			fmt.Fprintf(&b, "%d response.output_item.added %d %#v\n", e.SequenceNumber, e.OutputIndex, e.Item)
		case protocol.OutputItemDone:
			fmt.Fprintf(&b, "%d response.output_item.done %d %#v\n", e.SequenceNumber, e.OutputIndex, e.Item)
		case protocol.ContentPartAdded:
			fmt.Fprintf(&b, "%d response.content_part.added %d %d %s %#v\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Part)
		case protocol.ContentPartDone:
			fmt.Fprintf(&b, "%d response.content_part.done %d %d %s %#v\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Part)
		case protocol.OutputTextDelta:
			fmt.Fprintf(&b, "%d response.output_text.delta %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Delta)
		case protocol.OutputTextDone:
			fmt.Fprintf(&b, "%d response.output_text.done %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Text)
		case protocol.RefusalDelta:
			fmt.Fprintf(&b, "%d response.refusal.delta %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Delta)
		case protocol.RefusalDone:
			fmt.Fprintf(&b, "%d response.refusal.done %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Refusal)
		case protocol.FunctionCallArgumentsDelta:
			fmt.Fprintf(&b, "%d response.function_call_arguments.delta %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ItemID, e.Delta)
		case protocol.FunctionCallArgumentsDone:
			fmt.Fprintf(&b, "%d response.function_call_arguments.done %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ItemID, e.Arguments)
		default:
			fmt.Fprintf(&b, "%T\n", e)
		}
	}
	return strings.ReplaceAll(b.String(), "`", `\x60`)
}

// assertTrace fails t unless events render as want, a trace with its lines' leading tabs
// trimmed.
func assertTrace(t *testing.T, events []protocol.Event, want string) {
	t.Helper()
	var lines []string
	for line := range strings.Lines(strings.TrimSpace(want)) {
		lines = append(lines, strings.TrimLeft(line, "\t"))
	}
	if got, want := trace(events), strings.Join(lines, "")+"\n"; got != want {
		t.Errorf("events:\n%s\nwant:\n%s", got, want)
	}
}

// fold folds events with an Accumulator, failing t if it refuses one, and returns its Response.
func fold(t *testing.T, events []protocol.Event) *protocol.Response {
	t.Helper()
	var acc protocol.Accumulator
	for _, e := range events {
		if err := acc.Add(e); err != nil {
			t.Fatalf("Accumulator.Add: %v", err)
		}
	}
	return acc.Response()
}

// assertResponse fails t unless got and want are the same response.
func assertResponse(t *testing.T, got, want *protocol.Response) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Response = %#v\nwant       %#v", got, want)
	}
}

// asStreamed returns reply, an unstreamed reply's body, as the reply body, a text/event-stream
// reply, gave: its id the stream's first chunk's, and each tool call's id the one the stream gave
// the call in its place. The recorded replies were each sent twice, and the router makes new ids
// for each, so the streamed reply's ids aren't the other's.
func asStreamed(t *testing.T, reply, body string) string {
	t.Helper()
	var r map[string]any
	if err := json.Unmarshal([]byte(reply), &r); err != nil {
		t.Fatal(err)
	}
	r["id"] = firstChunkID(t, body)
	callIDs := streamCallIDs(t, body)
	for _, c := range r["choices"].([]any) {
		m := c.(map[string]any)["message"].(map[string]any)
		calls, _ := m["tool_calls"].([]any)
		if len(calls) != len(callIDs) {
			t.Fatalf("the reply has %d tool calls, the stream %d", len(calls), len(callIDs))
		}
		for i, call := range calls {
			call.(map[string]any)["id"] = callIDs[i]
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// firstChunkID returns the id of the first chunk of body, a text/event-stream reply.
func firstChunkID(t *testing.T, body string) string {
	t.Helper()
	e, err := sse.NewDecoder(strings.NewReader(body)).Next()
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(e.Data), &c); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

// streamCallIDs returns the ids of the tool calls in body, a text/event-stream reply, in the
// order of their indexes: each call's id, which the fragments that carry one give.
func streamCallIDs(t *testing.T, body string) []string {
	t.Helper()
	var ids []string
	set := func(index int, id string) {
		for len(ids) <= index {
			ids = append(ids, "")
		}
		ids[index] = id
	}
	events := sse.NewDecoder(strings.NewReader(body))
	for {
		e, err := events.Next()
		if errors.Is(err, io.EOF) {
			return ids
		}
		if err != nil {
			t.Fatal(err)
		}
		if e.Data == "[DONE]" {
			continue
		}
		var c struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Index int    `json:"index"`
						ID    string `json:"id"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(e.Data), &c); err != nil {
			t.Fatal(err)
		}
		for _, choice := range c.Choices {
			for _, call := range choice.Delta.ToolCalls {
				if call.ID != "" {
					set(call.Index, call.ID)
				}
			}
		}
	}
}

// TestStreamRecorded translates the recorded router streams to events, and folds them to the
// Response DecodeResponse gives for the same reply unstreamed, item ids included.
func TestStreamRecorded(t *testing.T) {
	cases := []struct {
		name  string
		trace string
	}{
		{"text", `
			0 response.created in_progress
			1 response.output_item.added 0 protocol.Reasoning{ID:"chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
			2 response.content_part.added 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 protocol.ReasoningText{Text:""}
			3-12 response.reasoning.delta 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0, 10 deltas
			13 response.reasoning.done 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 "We just output three French cities separated by commas."
			14 response.content_part.done 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 protocol.ReasoningText{Text:"We just output three French cities separated by commas."}
			15 response.output_item.done 0 protocol.Reasoning{ID:"chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"We just output three French cities separated by commas."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:completions/v1:eyJyZWFzb25pbmdfY29udGVudCI6IldlIGp1c3Qgb3V0cHV0IHRocmVlIEZyZW5jaCBjaXRpZXMgc2VwYXJhdGVkIGJ5IGNvbW1hcy4ifQ=="}
			16 response.output_item.added 1 protocol.Message{ID:"chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/1", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
			17 response.content_part.added 1 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/1 protocol.OutputText{Text:""}
			18 response.output_text.delta 1 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/1 "Paris"
			19 response.output_text.delta 1 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/1 ","
			20 response.output_text.delta 1 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/1 " Lyon"
			21 response.output_text.delta 1 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/1 ","
			22 response.output_text.delta 1 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/1 " Marseille"
			23 response.output_text.done 1 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/1 "Paris, Lyon, Marseille"
			24 response.content_part.done 1 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/1 protocol.OutputText{Text:"Paris, Lyon, Marseille"}
			25 response.output_item.done 1 protocol.Message{ID:"chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/1", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Paris, Lyon, Marseille"}}}
			26 response.completed 2 items`},
		{"length", `
			0 response.created in_progress
			1 response.output_item.added 0 protocol.Message{ID:"chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
			2 response.content_part.added 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 protocol.OutputText{Text:""}
			3 response.output_text.delta 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 "1"
			4 response.output_text.delta 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 ","
			5 response.output_text.delta 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 " "
			6 response.output_text.delta 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 "2"
			7 response.output_text.delta 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 ","
			8 response.output_text.delta 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 " "
			9 response.output_text.delta 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 "3"
			10 response.output_text.delta 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 ","
			11 response.output_text.done 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 "1, 2, 3,"
			12 response.content_part.done 0 0 chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0 protocol.OutputText{Text:"1, 2, 3,"}
			13 response.output_item.done 0 protocol.Message{ID:"chatcmpl-Ua3L4FAciVzBKIrsejypEGgyaReDN4Rk/0", Status:"incomplete", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"1, 2, 3,"}}}
			14 response.incomplete 1 items`},
		{"tool_call", `
			0 response.created in_progress
			1 response.output_item.added 0 protocol.Reasoning{ID:"chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
			2 response.content_part.added 0 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 protocol.ReasoningText{Text:""}
			3-10 response.reasoning.delta 0 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0, 8 deltas
			11 response.reasoning.done 0 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 "We need to call function get_weather."
			12 response.content_part.done 0 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 protocol.ReasoningText{Text:"We need to call function get_weather."}
			13 response.output_item.done 0 protocol.Reasoning{ID:"chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"We need to call function get_weather."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:completions/v1:eyJyZWFzb25pbmdfY29udGVudCI6IldlIG5lZWQgdG8gY2FsbCBmdW5jdGlvbiBnZXRfd2VhdGhlci4ifQ=="}
			14 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1", Status:"in_progress", CallID:"LsrzvhpPKdbtJyriZMr6ivjrWBgmWA3j", Name:"get_weather", Arguments:""}
			15 response.function_call_arguments.delta 1 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1 "{"
			16 response.function_call_arguments.delta 1 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1 "\n  \""
			17 response.function_call_arguments.delta 1 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1 "city"
			18 response.function_call_arguments.delta 1 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1 "\":"
			19 response.function_call_arguments.delta 1 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1 " \""
			20 response.function_call_arguments.delta 1 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1 "Paris"
			21 response.function_call_arguments.delta 1 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1 "\""
			22 response.function_call_arguments.delta 1 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1 "\n}"
			23 response.function_call_arguments.done 1 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1 "{\n  \"city\": \"Paris\"\n}"
			24 response.output_item.done 1 protocol.FunctionCall{ID:"chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/1", Status:"completed", CallID:"LsrzvhpPKdbtJyriZMr6ivjrWBgmWA3j", Name:"get_weather", Arguments:"{\n  \"city\": \"Paris\"\n}"}
			25 response.completed 2 items`},
		{"parallel_tool_calls", `
			0 response.created in_progress
			1 response.output_item.added 0 protocol.Reasoning{ID:"chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
			2 response.content_part.added 0 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 protocol.ReasoningText{Text:""}
			3-97 response.reasoning.delta 0 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0, 95 deltas
			98 response.reasoning.done 0 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 "The user is asking for the current weather in two different cities: Paris and Tokyo.\nI have a tool \x60get_weather\x60 that takes a \x60city\x60 as an argument.\nThe user explicitly requested to call the tool once for each city, in parallel.\n\nPlan:\n1. Call \x60get_weather(city=\"Paris\")\x60.\n2. Call \x60get_weather(city=\"Tokyo\")\x60.\n3. Present the results to the user."
			99 response.content_part.done 0 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 protocol.ReasoningText{Text:"The user is asking for the current weather in two different cities: Paris and Tokyo.\nI have a tool \x60get_weather\x60 that takes a \x60city\x60 as an argument.\nThe user explicitly requested to call the tool once for each city, in parallel.\n\nPlan:\n1. Call \x60get_weather(city=\"Paris\")\x60.\n2. Call \x60get_weather(city=\"Tokyo\")\x60.\n3. Present the results to the user."}
			100 response.output_item.done 0 protocol.Reasoning{ID:"chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"The user is asking for the current weather in two different cities: Paris and Tokyo.\nI have a tool \x60get_weather\x60 that takes a \x60city\x60 as an argument.\nThe user explicitly requested to call the tool once for each city, in parallel.\n\nPlan:\n1. Call \x60get_weather(city=\"Paris\")\x60.\n2. Call \x60get_weather(city=\"Tokyo\")\x60.\n3. Present the results to the user."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:completions/v1:eyJyZWFzb25pbmdfY29udGVudCI6IlRoZSB1c2VyIGlzIGFza2luZyBmb3IgdGhlIGN1cnJlbnQgd2VhdGhlciBpbiB0d28gZGlmZmVyZW50IGNpdGllczogUGFyaXMgYW5kIFRva3lvLlxuSSBoYXZlIGEgdG9vbCBgZ2V0X3dlYXRoZXJgIHRoYXQgdGFrZXMgYSBgY2l0eWAgYXMgYW4gYXJndW1lbnQuXG5UaGUgdXNlciBleHBsaWNpdGx5IHJlcXVlc3RlZCB0byBjYWxsIHRoZSB0b29sIG9uY2UgZm9yIGVhY2ggY2l0eSwgaW4gcGFyYWxsZWwuXG5cblBsYW46XG4xLiBDYWxsIGBnZXRfd2VhdGhlcihjaXR5PVwiUGFyaXNcIilgLlxuMi4gQ2FsbCBgZ2V0X3dlYXRoZXIoY2l0eT1cIlRva3lvXCIpYC5cbjMuIFByZXNlbnQgdGhlIHJlc3VsdHMgdG8gdGhlIHVzZXIuIn0="}
			101 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1", Status:"in_progress", CallID:"zzwGjyJxRF0NLM9cl3nvl9X1uykH0HJn", Name:"get_weather", Arguments:""}
			102 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "{"
			103 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "\"city"
			104 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "\":"
			105 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "\""
			106 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "Paris"
			107 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "\""
			108 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "}"
			109 response.output_item.added 2 protocol.FunctionCall{ID:"chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/2", Status:"in_progress", CallID:"EXc5ifcsLJtON5nVlTOtEvXtYSHnWbgi", Name:"get_weather", Arguments:""}
			110 response.function_call_arguments.delta 2 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/2 "{"
			111 response.function_call_arguments.delta 2 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/2 "\"city"
			112 response.function_call_arguments.delta 2 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/2 "\":"
			113 response.function_call_arguments.delta 2 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/2 "\""
			114 response.function_call_arguments.delta 2 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/2 "Tokyo"
			115 response.function_call_arguments.delta 2 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/2 "\""
			116 response.function_call_arguments.delta 2 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/2 "}"
			117 response.function_call_arguments.done 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "{\"city\":\"Paris\"}"
			118 response.output_item.done 1 protocol.FunctionCall{ID:"chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1", Status:"completed", CallID:"zzwGjyJxRF0NLM9cl3nvl9X1uykH0HJn", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
			119 response.function_call_arguments.done 2 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/2 "{\"city\":\"Tokyo\"}"
			120 response.output_item.done 2 protocol.FunctionCall{ID:"chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/2", Status:"completed", CallID:"EXc5ifcsLJtON5nVlTOtEvXtYSHnWbgi", Name:"get_weather", Arguments:"{\"city\":\"Tokyo\"}"}
			121 response.completed 3 items`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := readTestdata(t, tc.name+".sse")
			r := decodeStream(t, body)
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			assertTrace(t, r.events, tc.trace)
			want, err := decode(asStreamed(t, readTestdata(t, tc.name+".json"), body))
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			assertResponse(t, fold(t, r.events), want)
		})
	}
}

// TestStreamUsage wants the usage of the chunk that carries it, after the finish_reason, in
// the terminal event's response.
func TestStreamUsage(t *testing.T) {
	r := decodeStream(t, readTestdata(t, "text.sse"))
	completed, ok := r.events[len(r.events)-1].(protocol.ResponseCompleted)
	if !ok {
		t.Fatalf("last event = %#v, want response.completed", r.events[len(r.events)-1])
	}
	want := protocol.Usage{InputTokens: 78, OutputTokens: 25, TotalTokens: 103}
	if completed.Response.Usage != want {
		t.Errorf("Usage = %+v, want %+v", completed.Response.Usage, want)
	}
}

// TestStreamParts covers a message's parts: a run of content and a run of refusal each make a
// part, in the order they arrive, and the message folds to what DecodeResponse gives.
func TestStreamParts(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		trace  string
		reply  string
	}{
		{
			name: "refusal",
			chunks: []string{
				delta(`{"role":"assistant","content":null}`, ""),
				delta(`{"refusal":"I can't"}`, ""),
				delta(`{"refusal":" help."}`, ""),
				delta(`{}`, "stop"),
				usageChunk, "[DONE]",
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Message{ID:"chatcmpl-1/0", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
				2 response.content_part.added 0 0 chatcmpl-1/0 protocol.Refusal{Refusal:""}
				3 response.refusal.delta 0 0 chatcmpl-1/0 "I can't"
				4 response.refusal.delta 0 0 chatcmpl-1/0 " help."
				5 response.refusal.done 0 0 chatcmpl-1/0 "I can't help."
				6 response.content_part.done 0 0 chatcmpl-1/0 protocol.Refusal{Refusal:"I can't help."}
				7 response.output_item.done 0 protocol.Message{ID:"chatcmpl-1/0", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.Refusal{Refusal:"I can't help."}}}
				8 response.completed 1 items`,
			reply: `{"id":"chatcmpl-1","choices":[{"message":{"refusal":"I can't help."},"finish_reason":"stop"}],
				"usage":{"completion_tokens":5,"prompt_tokens":7,"total_tokens":12}}`,
		},
		{
			name: "content, then refusal",
			chunks: []string{
				delta(`{"content":"Well"}`, ""),
				delta(`{"content":"","refusal":"No."}`, "content_filter"),
				usageChunk, "[DONE]",
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Message{ID:"chatcmpl-1/0", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
				2 response.content_part.added 0 0 chatcmpl-1/0 protocol.OutputText{Text:""}
				3 response.output_text.delta 0 0 chatcmpl-1/0 "Well"
				4 response.output_text.done 0 0 chatcmpl-1/0 "Well"
				5 response.content_part.done 0 0 chatcmpl-1/0 protocol.OutputText{Text:"Well"}
				6 response.content_part.added 0 1 chatcmpl-1/0 protocol.Refusal{Refusal:""}
				7 response.refusal.delta 0 1 chatcmpl-1/0 "No."
				8 response.refusal.done 0 1 chatcmpl-1/0 "No."
				9 response.content_part.done 0 1 chatcmpl-1/0 protocol.Refusal{Refusal:"No."}
				10 response.output_item.done 0 protocol.Message{ID:"chatcmpl-1/0", Status:"incomplete", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Well"}, protocol.Refusal{Refusal:"No."}}}
				11 response.incomplete 1 items`,
			reply: `{"id":"chatcmpl-1","choices":[{"message":{"content":"Well","refusal":"No."},"finish_reason":"content_filter"}],
				"usage":{"completion_tokens":5,"prompt_tokens":7,"total_tokens":12}}`,
		},
		{
			name: "no content",
			chunks: []string{
				delta(`{"role":"assistant","content":null}`, ""),
				delta(`{"content":""}`, ""),
				delta(`{}`, "stop"),
				usageChunk, "[DONE]",
			},
			trace: `
				0 response.created in_progress
				1 response.completed 0 items`,
			reply: `{"id":"chatcmpl-1","choices":[{"message":{"content":""},"finish_reason":"stop"}],
				"usage":{"completion_tokens":5,"prompt_tokens":7,"total_tokens":12}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.chunks...))
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			assertTrace(t, r.events, tc.trace)
			want, err := decode(tc.reply)
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			assertResponse(t, fold(t, r.events), want)
		})
	}
}

// callDelta is a delta of one tool call fragment, of index: the call's first fragment when id is
// set, carrying id, type function, and name, and a later one, of arguments alone, otherwise.
func callDelta(index int, id, name, arguments string) string {
	args, err := json.Marshal(arguments)
	if err != nil {
		panic(err)
	}
	if id == "" {
		return fmt.Sprintf(`{"tool_calls":[{"index":%d,"function":{"arguments":%s}}]}`, index, args)
	}
	return fmt.Sprintf(`{"tool_calls":[{"index":%d,"id":%q,"type":"function","function":{"name":%q,"arguments":%s}}]}`, index, id, name, args)
}

// parisTokyo is reply chatcmpl-1 of two calls, c1 for Paris and c2 for Tokyo, unstreamed.
const parisTokyo = `{"id":"chatcmpl-1","choices":[{"message":{"tool_calls":[
		{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}},
		{"id":"c2","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]},
	"finish_reason":"tool_calls"}]}`

// TestStreamToolCallsInterleavedFoldAsSequential wants calls whose fragments interleave folded to
// the Response the same calls give sent one after the other.
func TestStreamToolCallsInterleavedFoldAsSequential(t *testing.T) {
	sequential := decodeStream(t, stream(
		delta(callDelta(0, "c1", "get_weather", `{"city":`), ""),
		delta(callDelta(0, "", "", `"Paris"}`), ""),
		delta(callDelta(1, "c2", "get_weather", `{"city":`), ""),
		delta(callDelta(1, "", "", `"Tokyo"}`), ""),
		delta(`{}`, "tool_calls"),
	))
	interleaved := decodeStream(t, stream(
		delta(callDelta(0, "c1", "get_weather", `{"city":`), ""),
		delta(callDelta(1, "c2", "get_weather", `{"city":`), ""),
		delta(callDelta(0, "", "", `"Paris"}`), ""),
		delta(callDelta(1, "", "", `"Tokyo"}`), ""),
		delta(`{}`, "tool_calls"),
	))
	if sequential.err != nil || interleaved.err != nil {
		t.Fatalf("stream errors = %v, %v", sequential.err, interleaved.err)
	}
	assertResponse(t, fold(t, interleaved.events), fold(t, sequential.events))
}

// TestStreamToolCalls covers function calls: the first call closes the message ahead of it,
// completed; calls stay open together, their fragments assembled by index in whatever order they
// arrive, interleaved, several in one chunk, or begun out of index order, each at the output
// index its tool-call index gives; at the finish_reason they close in index order, the last
// taking the status it gives, those before it completed; and each folds to what DecodeResponse
// gives for the same reply, which the same calls sent one after another give too.
func TestStreamToolCalls(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		trace  string
		reply  string
	}{
		{
			name: "a message, then a call",
			chunks: []string{
				delta(`{"content":"Checking."}`, ""),
				delta(callDelta(0, "c1", "get_weather", `{"city":`), ""),
				delta(callDelta(0, "", "", `"Paris"}`), ""),
				delta(`{}`, "tool_calls"),
				usageChunk, "[DONE]",
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Message{ID:"chatcmpl-1/0", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
				2 response.content_part.added 0 0 chatcmpl-1/0 protocol.OutputText{Text:""}
				3 response.output_text.delta 0 0 chatcmpl-1/0 "Checking."
				4 response.output_text.done 0 0 chatcmpl-1/0 "Checking."
				5 response.content_part.done 0 0 chatcmpl-1/0 protocol.OutputText{Text:"Checking."}
				6 response.output_item.done 0 protocol.Message{ID:"chatcmpl-1/0", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Checking."}}}
				7 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"in_progress", CallID:"c1", Name:"get_weather", Arguments:""}
				8 response.function_call_arguments.delta 1 chatcmpl-1/1 "{\"city\":"
				9 response.function_call_arguments.delta 1 chatcmpl-1/1 "\"Paris\"}"
				10 response.function_call_arguments.done 1 chatcmpl-1/1 "{\"city\":\"Paris\"}"
				11 response.output_item.done 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"completed", CallID:"c1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
				12 response.completed 2 items`,
			reply: `{"id":"chatcmpl-1","choices":[{"message":{"content":"Checking.","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
				"finish_reason":"tool_calls"}],"usage":{"completion_tokens":5,"prompt_tokens":7,"total_tokens":12}}`,
		},
		{
			name: "two calls cut off at the bound, only the last incomplete",
			chunks: []string{
				delta(callDelta(0, "c1", "get_weather", `{"city":"Paris"}`), ""),
				delta(callDelta(1, "c2", "get_weather", `{"ci`), ""),
				delta(`{}`, "length"),
				usageChunk, "[DONE]",
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.FunctionCall{ID:"chatcmpl-1/0", Status:"in_progress", CallID:"c1", Name:"get_weather", Arguments:""}
				2 response.function_call_arguments.delta 0 chatcmpl-1/0 "{\"city\":\"Paris\"}"
				3 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"in_progress", CallID:"c2", Name:"get_weather", Arguments:""}
				4 response.function_call_arguments.delta 1 chatcmpl-1/1 "{\"ci"
				5 response.function_call_arguments.done 0 chatcmpl-1/0 "{\"city\":\"Paris\"}"
				6 response.output_item.done 0 protocol.FunctionCall{ID:"chatcmpl-1/0", Status:"completed", CallID:"c1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
				7 response.function_call_arguments.done 1 chatcmpl-1/1 "{\"ci"
				8 response.output_item.done 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"incomplete", CallID:"c2", Name:"get_weather", Arguments:"{\"ci"}
				9 response.incomplete 2 items`,
			reply: `{"id":"chatcmpl-1","choices":[{"message":{"tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}},
					{"id":"c2","type":"function","function":{"name":"get_weather","arguments":"{\"ci"}}]},
				"finish_reason":"length"}],"usage":{"completion_tokens":5,"prompt_tokens":7,"total_tokens":12}}`,
		},
		{
			name: "a call of no arguments, its first fragment repeated",
			chunks: []string{
				delta(callDelta(0, "c1", "get_time", ""), ""),
				delta(callDelta(0, "c1", "get_time", ""), ""),
				delta(`{}`, "tool_calls"),
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.FunctionCall{ID:"chatcmpl-1/0", Status:"in_progress", CallID:"c1", Name:"get_time", Arguments:""}
				2 response.function_call_arguments.done 0 chatcmpl-1/0 ""
				3 response.output_item.done 0 protocol.FunctionCall{ID:"chatcmpl-1/0", Status:"completed", CallID:"c1", Name:"get_time", Arguments:""}
				4 response.completed 1 items`,
			reply: `{"id":"chatcmpl-1","choices":[{"message":{"tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_time","arguments":""}}]},
				"finish_reason":"tool_calls"}]}`,
		},
		{
			name: "two calls interleaved, 0, 1, 0, 1",
			chunks: []string{
				delta(callDelta(0, "c1", "get_weather", `{"city":`), ""),
				delta(callDelta(1, "c2", "get_weather", `{"city":`), ""),
				delta(callDelta(0, "", "", `"Paris"}`), ""),
				delta(callDelta(1, "", "", `"Tokyo"}`), ""),
				delta(`{}`, "tool_calls"),
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.FunctionCall{ID:"chatcmpl-1/0", Status:"in_progress", CallID:"c1", Name:"get_weather", Arguments:""}
				2 response.function_call_arguments.delta 0 chatcmpl-1/0 "{\"city\":"
				3 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"in_progress", CallID:"c2", Name:"get_weather", Arguments:""}
				4 response.function_call_arguments.delta 1 chatcmpl-1/1 "{\"city\":"
				5 response.function_call_arguments.delta 0 chatcmpl-1/0 "\"Paris\"}"
				6 response.function_call_arguments.delta 1 chatcmpl-1/1 "\"Tokyo\"}"
				7 response.function_call_arguments.done 0 chatcmpl-1/0 "{\"city\":\"Paris\"}"
				8 response.output_item.done 0 protocol.FunctionCall{ID:"chatcmpl-1/0", Status:"completed", CallID:"c1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
				9 response.function_call_arguments.done 1 chatcmpl-1/1 "{\"city\":\"Tokyo\"}"
				10 response.output_item.done 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"completed", CallID:"c2", Name:"get_weather", Arguments:"{\"city\":\"Tokyo\"}"}
				11 response.completed 2 items`,
			reply: parisTokyo,
		},
		{
			name: "one chunk of call 1's fragment before call 0's",
			chunks: []string{
				// One line, as a data event's.
				delta(`{"tool_calls":[`+
					`{"index":1,"id":"c2","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}},`+
					`{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}`, ""),
				delta(`{}`, "tool_calls"),
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"in_progress", CallID:"c2", Name:"get_weather", Arguments:""}
				2 response.function_call_arguments.delta 1 chatcmpl-1/1 "{\"city\":\"Tokyo\"}"
				3 response.output_item.added 0 protocol.FunctionCall{ID:"chatcmpl-1/0", Status:"in_progress", CallID:"c1", Name:"get_weather", Arguments:""}
				4 response.function_call_arguments.delta 0 chatcmpl-1/0 "{\"city\":\"Paris\"}"
				5 response.function_call_arguments.done 0 chatcmpl-1/0 "{\"city\":\"Paris\"}"
				6 response.output_item.done 0 protocol.FunctionCall{ID:"chatcmpl-1/0", Status:"completed", CallID:"c1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
				7 response.function_call_arguments.done 1 chatcmpl-1/1 "{\"city\":\"Tokyo\"}"
				8 response.output_item.done 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"completed", CallID:"c2", Name:"get_weather", Arguments:"{\"city\":\"Tokyo\"}"}
				9 response.completed 2 items`,
			reply: parisTokyo,
		},
		{
			name: "after reasoning, call 1 begun before call 0, cut off at the bound",
			chunks: []string{
				delta(`{"reasoning_content":"Hm."}`, ""),
				delta(callDelta(1, "c2", "get_weather", `{"city":`), ""),
				delta(callDelta(0, "c1", "get_weather", `{"city":"Paris"}`), ""),
				delta(callDelta(1, "", "", `"Tok`), ""),
				delta(`{}`, "length"),
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Reasoning{ID:"chatcmpl-1/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
				2 response.content_part.added 0 0 chatcmpl-1/0 protocol.ReasoningText{Text:""}
				3 response.reasoning.delta 0 0 chatcmpl-1/0 "Hm."
				4 response.reasoning.done 0 0 chatcmpl-1/0 "Hm."
				5 response.content_part.done 0 0 chatcmpl-1/0 protocol.ReasoningText{Text:"Hm."}
				6 response.output_item.done 0 protocol.Reasoning{ID:"chatcmpl-1/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"Hm."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:completions/v1:eyJyZWFzb25pbmdfY29udGVudCI6IkhtLiJ9"}
				7 response.output_item.added 2 protocol.FunctionCall{ID:"chatcmpl-1/2", Status:"in_progress", CallID:"c2", Name:"get_weather", Arguments:""}
				8 response.function_call_arguments.delta 2 chatcmpl-1/2 "{\"city\":"
				9 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"in_progress", CallID:"c1", Name:"get_weather", Arguments:""}
				10 response.function_call_arguments.delta 1 chatcmpl-1/1 "{\"city\":\"Paris\"}"
				11 response.function_call_arguments.delta 2 chatcmpl-1/2 "\"Tok"
				12 response.function_call_arguments.done 1 chatcmpl-1/1 "{\"city\":\"Paris\"}"
				13 response.output_item.done 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"completed", CallID:"c1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
				14 response.function_call_arguments.done 2 chatcmpl-1/2 "{\"city\":\"Tok"
				15 response.output_item.done 2 protocol.FunctionCall{ID:"chatcmpl-1/2", Status:"incomplete", CallID:"c2", Name:"get_weather", Arguments:"{\"city\":\"Tok"}
				16 response.incomplete 3 items`,
			reply: `{"id":"chatcmpl-1","choices":[{"message":{"reasoning_content":"Hm.","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}},
					{"id":"c2","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tok"}}]},
				"finish_reason":"length"}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.chunks...))
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			assertTrace(t, r.events, tc.trace)
			want, err := decode(tc.reply)
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			assertResponse(t, fold(t, r.events), want)
		})
	}
}

// TestStreamReasoning covers the reasoning item: it comes first, its one reasoning_text part
// streamed as reasoning deltas, added in progress; it closes, completed, when the message or a
// call begins, or at the finish_reason, of the status it gives; its output_item.done carries the
// envelope DecodeResponse gives the same reasoning; and it folds to what DecodeResponse gives for
// the same reply.
func TestStreamReasoning(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		trace  string
		reply  string
	}{
		{
			name: "reasoning and content in one chunk",
			chunks: []string{
				delta(`{"role":"assistant","content":null}`, ""),
				delta(`{"reasoning_content":"The user"}`, ""),
				delta(`{"reasoning_content":" asks.","content":"Paris"}`, ""),
				delta(`{}`, "stop"),
				usageChunk, "[DONE]",
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Reasoning{ID:"chatcmpl-1/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
				2 response.content_part.added 0 0 chatcmpl-1/0 protocol.ReasoningText{Text:""}
				3-4 response.reasoning.delta 0 0 chatcmpl-1/0, 2 deltas
				5 response.reasoning.done 0 0 chatcmpl-1/0 "The user asks."
				6 response.content_part.done 0 0 chatcmpl-1/0 protocol.ReasoningText{Text:"The user asks."}
				7 response.output_item.done 0 protocol.Reasoning{ID:"chatcmpl-1/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"The user asks."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"` + envelopeUserAsks + `"}
				8 response.output_item.added 1 protocol.Message{ID:"chatcmpl-1/1", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
				9 response.content_part.added 1 0 chatcmpl-1/1 protocol.OutputText{Text:""}
				10 response.output_text.delta 1 0 chatcmpl-1/1 "Paris"
				11 response.output_text.done 1 0 chatcmpl-1/1 "Paris"
				12 response.content_part.done 1 0 chatcmpl-1/1 protocol.OutputText{Text:"Paris"}
				13 response.output_item.done 1 protocol.Message{ID:"chatcmpl-1/1", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Paris"}}}
				14 response.completed 2 items`,
			reply: `{"id":"chatcmpl-1","choices":[{"message":{"content":"Paris","reasoning_content":"The user asks."},"finish_reason":"stop"}],
				"usage":{"completion_tokens":5,"prompt_tokens":7,"total_tokens":12}}`,
		},
		{
			name: "reasoning, then a call",
			chunks: []string{
				delta(`{"reasoning_content":"Need the weather tool."}`, ""),
				delta(callDelta(0, "c1", "get_weather", `{"city":"Paris"}`), ""),
				delta(`{}`, "tool_calls"),
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Reasoning{ID:"chatcmpl-1/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
				2 response.content_part.added 0 0 chatcmpl-1/0 protocol.ReasoningText{Text:""}
				3 response.reasoning.delta 0 0 chatcmpl-1/0 "Need the weather tool."
				4 response.reasoning.done 0 0 chatcmpl-1/0 "Need the weather tool."
				5 response.content_part.done 0 0 chatcmpl-1/0 protocol.ReasoningText{Text:"Need the weather tool."}
				6 response.output_item.done 0 protocol.Reasoning{ID:"chatcmpl-1/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"Need the weather tool."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"` + envelopeWeatherTool + `"}
				7 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"in_progress", CallID:"c1", Name:"get_weather", Arguments:""}
				8 response.function_call_arguments.delta 1 chatcmpl-1/1 "{\"city\":\"Paris\"}"
				9 response.function_call_arguments.done 1 chatcmpl-1/1 "{\"city\":\"Paris\"}"
				10 response.output_item.done 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"completed", CallID:"c1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
				11 response.completed 2 items`,
			reply: `{"id":"chatcmpl-1","choices":[{"message":{"reasoning_content":"Need the weather tool.","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
				"finish_reason":"tool_calls"}]}`,
		},
		{
			name: "reasoning alone, cut off at the bound, the item incomplete",
			chunks: []string{
				delta(`{"reasoning_content":"The user"}`, ""),
				delta(`{"reasoning_content":" asks."}`, "length"),
				usageChunk,
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Reasoning{ID:"chatcmpl-1/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
				2 response.content_part.added 0 0 chatcmpl-1/0 protocol.ReasoningText{Text:""}
				3-4 response.reasoning.delta 0 0 chatcmpl-1/0, 2 deltas
				5 response.reasoning.done 0 0 chatcmpl-1/0 "The user asks."
				6 response.content_part.done 0 0 chatcmpl-1/0 protocol.ReasoningText{Text:"The user asks."}
				7 response.output_item.done 0 protocol.Reasoning{ID:"chatcmpl-1/0", Status:"incomplete", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"The user asks."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"` + envelopeUserAsks + `"}
				8 response.incomplete 1 items`,
			reply: `{"id":"chatcmpl-1","choices":[{"message":{"content":"","reasoning_content":"The user asks."},"finish_reason":"length"}],
				"usage":{"completion_tokens":5,"prompt_tokens":7,"total_tokens":12}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.chunks...))
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			assertTrace(t, r.events, tc.trace)
			want, err := decode(tc.reply)
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			assertResponse(t, fold(t, r.events), want)
		})
	}
}

// TestStreamReasoningRoundTrip folds the recorded reasoning and tool call stream, sends its
// output back with the call's output, and wants the reasoning the stream carried on the assistant
// message that makes the call, as an unstreamed reply's would be.
func TestStreamReasoningRoundTrip(t *testing.T) {
	r := decodeStream(t, readTestdata(t, "tool_call.sse"))
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	resp := fold(t, r.events)
	call, ok := resp.Output[1].(protocol.FunctionCall)
	if !ok {
		t.Fatalf("Output[1] = %#v, want the function call", resp.Output[1])
	}
	req := &protocol.Request{
		Model: "m",
		Input: []protocol.Item{text(protocol.RoleUser, "What is the weather in Paris right now?")},
	}
	req.Input = append(req.Input, resp.Output...)
	req.Input = append(req.Input, protocol.FunctionCallOutput{CallID: call.CallID, Output: "21"})
	assertJSON(t, encode(t, req), `{"model":"m","stream":false,"messages":[
		{"role":"user","content":"What is the weather in Paris right now?"},
		{"role":"assistant","reasoning_content":"We need to call function get_weather.","tool_calls":[
			{"id":"LsrzvhpPKdbtJyriZMr6ivjrWBgmWA3j","type":"function",
				"function":{"name":"get_weather","arguments":"{\n  \"city\": \"Paris\"\n}"}}]},
		{"role":"tool","tool_call_id":"LsrzvhpPKdbtJyriZMr6ivjrWBgmWA3j","content":"21"}]}`)
}

// TestStreamNoReplyID wants a stream whose chunks carry no id to give its message no id, as
// DecodeResponse gives the same reply: there is nothing to derive one from.
func TestStreamNoReplyID(t *testing.T) {
	noID := func(chunk string) string { return strings.Replace(chunk, `"id":"chatcmpl-1",`, "", 1) }
	r := decodeStream(t, stream(noID(delta(`{"content":"Paris"}`, "")), noID(delta(`{}`, "stop")), "[DONE]"))
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	for _, e := range r.events {
		if d, ok := e.(protocol.OutputTextDelta); ok && d.ItemID != "" {
			t.Errorf("output_text.delta item_id = %q, want none", d.ItemID)
		}
	}
	want, err := decode(`{"choices":[{"message":{"content":"Paris"},"finish_reason":"stop"}]}`)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	assertResponse(t, fold(t, r.events), want)
}

// TestStreamEnd covers how a stream ends: at [DONE], or at the stream's end once the
// finish_reason has arrived, and io.ErrUnexpectedEOF before it.
func TestStreamEnd(t *testing.T) {
	text := delta(`{"content":"Paris"}`, "")
	cases := []struct {
		name     string
		chunks   []string
		complete bool
	}{
		{"[DONE]", []string{text, delta(`{}`, "stop"), usageChunk, "[DONE]"}, true},
		{"end after the usage, without [DONE]", []string{text, delta(`{}`, "stop"), usageChunk}, true},
		{"end after the finish_reason, without usage or [DONE]", []string{text, delta(`{}`, "stop")}, true},
		{"end before the finish_reason", []string{text}, false},
		{"[DONE] before the finish_reason", []string{text, "[DONE]"}, false},
		{"empty stream", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.chunks...))
			if !tc.complete {
				if !errors.Is(r.err, io.ErrUnexpectedEOF) {
					t.Errorf("stream error = %v, want io.ErrUnexpectedEOF", r.err)
				}
				return
			}
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			last := r.events[len(r.events)-1]
			if c, ok := last.(protocol.ResponseCompleted); !ok || len(c.Response.Output) != 1 {
				t.Errorf("last event = %#v, want response.completed with the message", last)
			}
		})
	}
}

// TestStreamEndMakesNothingAfterTheTerminalEvent wants End to make nothing once [DONE] has made
// the terminal event.
func TestStreamEndMakesNothingAfterTheTerminalEvent(t *testing.T) {
	decoder := completions.New().NewStreamDecoder()
	for _, data := range []string{delta(`{"content":"Paris"}`, "stop"), "[DONE]"} {
		if _, err := decoder.Decode([]byte(data)); err != nil {
			t.Fatalf("Decode(%s): %v", data, err)
		}
	}
	if events, err := decoder.End(); events != nil || err != nil {
		t.Errorf("End = %v, %v; want nothing", events, err)
	}
}

func TestStreamRefusesChunk(t *testing.T) {
	text := delta(`{"content":"Paris"}`, "")
	cases := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"not JSON", []string{text, `{"choices":`}, "decode chunk"},
		{"unknown finish_reason", []string{text, delta(`{}`, "tired")}, `unknown finish_reason "tired"`},
		{"delta after the finish_reason", []string{delta(`{}`, "stop"), text}, "delta after the finish_reason"},
		{"second finish_reason", []string{text, delta(`{}`, "stop"), delta(`{}`, "stop")}, "a second finish_reason"},
		{"chunk of another reply", []string{text, strings.Replace(text, "chatcmpl-1", "chatcmpl-2", 1)}, `chunk of reply "chatcmpl-2" in the stream of reply "chatcmpl-1"`},
		{"chunk of no id after one of an id", []string{text, strings.Replace(text, `"id":"chatcmpl-1",`, "", 1)}, `chunk of reply "" in the stream of reply "chatcmpl-1"`},
		{"event after [DONE]", []string{text, delta(`{}`, "stop"), "[DONE]", usageChunk}, "event after the end of the stream"},
		{"a tool call after the finish_reason", []string{text, delta(`{}`, "stop"), delta(callDelta(0, "c1", "get_weather", "{}"), "")}, "delta after the finish_reason"},
		{"a fragment naming another call after a later call began", []string{delta(callDelta(0, "c1", "get_weather", "{"), ""), delta(callDelta(1, "c2", "get_time", "{}"), ""), delta(callDelta(0, "c9", "get_weather", "}"), "")}, `a fragment of tool call 0 names call "c9"`},
		{"a fragment naming another function", []string{delta(callDelta(0, "c1", "get_weather", "{"), ""), delta(`{"tool_calls":[{"index":0,"function":{"name":"get_time","arguments":"}"}}]}`, "")}, `a fragment of tool call 0 names call "" of "get_time"`},
		{"a fragment of a negative index", []string{delta(callDelta(-1, "c1", "get_weather", "{}"), "")}, "tool call of negative index -1"},
		{"calls whose indexes skip one", []string{delta(callDelta(0, "c1", "get_weather", "{}"), ""), delta(callDelta(2, "c3", "get_weather", "{}"), ""), delta(`{}`, "tool_calls")}, "tool call 2 began, but tool call 1 didn't"},
		{"message text after a call, a later call open", []string{delta(callDelta(1, "c2", "get_weather", "{}"), ""), delta(callDelta(0, "c1", "get_weather", "{}"), ""), text}, "message text after a tool call"},
		{"a first fragment of no name", []string{delta(callDelta(0, "c1", "", "{}"), "")}, "tool call 0's first fragment has no function name"},
		{"a first fragment of another type", []string{delta(strings.Replace(callDelta(0, "c1", "get_weather", "{}"), `"function"`, `"custom"`, 1), "")}, `tool call 0 has unknown type "custom"`},
		{"a fragment naming another call", []string{delta(callDelta(0, "c1", "get_weather", "{"), ""), delta(callDelta(0, "c2", "get_weather", "}"), "")}, `a fragment of tool call 0 names call "c2"`},
		{"message text after a call", []string{delta(callDelta(0, "c1", "get_weather", "{}"), ""), text}, "message text after a tool call"},
		{"reasoning after the message", []string{text, delta(`{"reasoning_content":"Hm."}`, "")}, "reasoning after the message or a tool call"},
		{"reasoning after a call", []string{delta(callDelta(0, "c1", "get_weather", "{}"), ""), delta(`{"reasoning_content":"Hm."}`, "")}, "reasoning after the message or a tool call"},
		{"reasoning after the finish_reason", []string{text, delta(`{}`, "stop"), delta(`{"reasoning_content":"Hm."}`, "")}, "delta after the finish_reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoder := completions.New().NewStreamDecoder()
			var err error
			for _, c := range tc.chunks {
				if _, err = decoder.Decode([]byte(c)); err != nil {
					break
				}
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Decode error = %v, want one containing %q", err, tc.want)
			}
			if _, err := decoder.Decode([]byte(text)); err == nil {
				t.Error("Decode after an error = nil, want the stream broken")
			}
		})
	}
}

// TestStreamErrorFrame wants an error frame, llama.cpp's report that generation failed
// mid-stream, to close the stream as the spec has it: the error event of the frame's type, code,
// message, and param, then response.failed, its response failed, its error the frame's code and
// message, holding the output made before the frame, the item open at it in progress with what
// it had, which is what the Accumulator held just before the frame. The frame alone carries no
// id; it comes as the first event, after response.created, or after chunks of an id, in each
// shape an OpenAI-style error body takes, and with each kind of item open. An event after it is
// an error, and End makes nothing.
func TestStreamErrorFrame(t *testing.T) {
	text := delta(`{"content":"Paris"}`, "")
	const llama = `{"error":{"code":500,"message":"context shift is disabled","type":"server_error"}}`
	message := func(content ...protocol.ContentPart) protocol.Item {
		return protocol.Message{ID: "chatcmpl-1/0", Status: protocol.StatusInProgress, Role: protocol.RoleAssistant, Content: content}
	}
	cases := []struct {
		name   string
		chunks []string
		// payload is the error event's, output the failed response's, and usage its usage.
		payload protocol.ErrorPayload
		output  []protocol.Item
		usage   protocol.Usage
		trace   string
	}{
		{name: "llama.cpp's, mid-stream", chunks: []string{text, llama},
			payload: protocol.ErrorPayload{Type: "server_error", Code: "500", Message: "context shift is disabled"},
			output:  []protocol.Item{message(protocol.OutputText{Text: "Paris"})},
			trace: `
			0 response.created in_progress
			1 response.output_item.added 0 protocol.Message{ID:"chatcmpl-1/0", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
			2 response.content_part.added 0 0 chatcmpl-1/0 protocol.OutputText{Text:""}
			3 response.output_text.delta 0 0 chatcmpl-1/0 "Paris"
			4 error protocol.ErrorPayload{Type:"server_error", Code:"500", Message:"context shift is disabled", Param:""}
			5 response.failed 1 items`},
		{name: "first, of a string code and a param", chunks: []string{`{"error":{"message":"no such model","type":"invalid_request_error","param":"model","code":"model_not_found"}}`},
			payload: protocol.ErrorPayload{Type: "invalid_request_error", Code: "model_not_found", Message: "no such model", Param: "model"},
			trace: `
			0 response.created in_progress
			1 error protocol.ErrorPayload{Type:"invalid_request_error", Code:"model_not_found", Message:"no such model", Param:"model"}
			2 response.failed 0 items`},
		{name: "a bare string", chunks: []string{text, `{"error":"model unloaded"}`},
			payload: protocol.ErrorPayload{Message: "model unloaded"},
			output:  []protocol.Item{message(protocol.OutputText{Text: "Paris"})}},
		{name: "of another shape", chunks: []string{text, `{"error":42}`},
			output: []protocol.Item{message(protocol.OutputText{Text: "Paris"})}},
		{name: "with a refusal part open after a text part", chunks: []string{text, delta(`{"refusal":"No"}`, ""), llama},
			payload: protocol.ErrorPayload{Type: "server_error", Code: "500", Message: "context shift is disabled"},
			output:  []protocol.Item{message(protocol.OutputText{Text: "Paris"}, protocol.Refusal{Refusal: "No"})}},
		{name: "with reasoning open", chunks: []string{delta(`{"reasoning_content":"Hm."}`, ""), llama},
			payload: protocol.ErrorPayload{Type: "server_error", Code: "500", Message: "context shift is disabled"},
			output:  []protocol.Item{protocol.Reasoning{ID: "chatcmpl-1/0", Status: protocol.StatusInProgress, Content: []protocol.ReasoningText{{Text: "Hm."}}}}},
		{name: "with a call open after the message", chunks: []string{text, delta(callDelta(0, "c1", "get_weather", `{"city":`), ""), llama},
			payload: protocol.ErrorPayload{Type: "server_error", Code: "500", Message: "context shift is disabled"},
			output: []protocol.Item{
				protocol.Message{ID: "chatcmpl-1/0", Status: protocol.StatusCompleted, Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}}},
				protocol.FunctionCall{ID: "chatcmpl-1/1", Status: protocol.StatusInProgress, CallID: "c1", Name: "get_weather", Arguments: `{"city":`},
			}},
		{name: "with two calls open, begun out of index order", chunks: []string{delta(callDelta(1, "c2", "get_weather", `{"city":`), ""), delta(callDelta(0, "c1", "get_weather", `{"ci`), ""), llama},
			payload: protocol.ErrorPayload{Type: "server_error", Code: "500", Message: "context shift is disabled"},
			output: []protocol.Item{
				protocol.FunctionCall{ID: "chatcmpl-1/0", Status: protocol.StatusInProgress, CallID: "c1", Name: "get_weather", Arguments: `{"ci`},
				protocol.FunctionCall{ID: "chatcmpl-1/1", Status: protocol.StatusInProgress, CallID: "c2", Name: "get_weather", Arguments: `{"city":`},
			}},
		{name: "with a later call open before an earlier one began", chunks: []string{delta(callDelta(1, "c2", "get_weather", `{"city":`), ""), llama},
			payload: protocol.ErrorPayload{Type: "server_error", Code: "500", Message: "context shift is disabled"},
			output: []protocol.Item{
				protocol.FunctionCall{ID: "chatcmpl-1/1", Status: protocol.StatusInProgress, CallID: "c2", Name: "get_weather", Arguments: `{"city":`},
			}},
		{name: "after the finish_reason and the usage", chunks: []string{text, delta(`{}`, "stop"), usageChunk, llama},
			payload: protocol.ErrorPayload{Type: "server_error", Code: "500", Message: "context shift is disabled"},
			output:  []protocol.Item{protocol.Message{ID: "chatcmpl-1/0", Status: protocol.StatusCompleted, Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}}}},
			usage:   protocol.Usage{InputTokens: 7, OutputTokens: 5, TotalTokens: 12}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.chunks...))
			if r.err != nil {
				t.Fatalf("stream error = %v, want the stream closed with response.failed", r.err)
			}
			if tc.trace != "" {
				assertTrace(t, r.events, tc.trace)
			}
			n := len(r.events)
			if n < 2 {
				t.Fatalf("%d events, want the error event and response.failed last", n)
			}
			e, ok := r.events[n-2].(protocol.ErrorEvent)
			if !ok || e.Error != tc.payload {
				t.Errorf("next to last event = %#v, want the error event of %#v", r.events[n-2], tc.payload)
			}
			failed, ok := r.events[n-1].(protocol.ResponseFailed)
			if !ok {
				t.Fatalf("last event = %#v, want response.failed", r.events[n-1])
			}
			want := &protocol.Response{
				Output: tc.output,
				Status: protocol.StatusFailed,
				Error:  &protocol.ResponseError{Code: tc.payload.Code, Message: tc.payload.Message},
				Usage:  tc.usage,
			}
			assertResponse(t, failed.Response, want)
			assertResponse(t, fold(t, r.events), want)
			// The failed response's output is what the Accumulator held just before the frame.
			before := fold(t, r.events[:n-2])
			if !reflect.DeepEqual(before.Output, tc.output) {
				t.Errorf("output before the frame = %#v\nwant the failed response's %#v", before.Output, tc.output)
			}

			decoder := completions.New().NewStreamDecoder()
			for _, c := range tc.chunks {
				if _, err := decoder.Decode([]byte(c)); err != nil {
					t.Fatalf("Decode: %v", err)
				}
			}
			if events, err := decoder.Decode([]byte(text)); err == nil || len(events) > 0 {
				t.Errorf("Decode after the error frame = %v, %v; want no events and the stream ended", events, err)
			}
			if events, err := decoder.End(); err != nil || len(events) > 0 {
				t.Errorf("End after the error frame = %v, %v; want nothing", events, err)
			}
		})
	}
}

// TestStreamDecodeErrorIsNoEndOfStream wants a chunk cut short, whose JSON error wraps
// io.ErrUnexpectedEOF, reported as a chunk the format can't decode, not as a stream that ended
// early.
func TestStreamDecodeErrorIsNoEndOfStream(t *testing.T) {
	_, err := completions.New().NewStreamDecoder().Decode([]byte(`{"choices":`))
	if err == nil || !strings.Contains(err.Error(), "decode chunk") || errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Decode error = %v, want a decode error that doesn't match io.ErrUnexpectedEOF", err)
	}
}

// TestStreamReturnsEventsBeforeAnError wants the events a chunk made before its error: the
// first chunk opens the stream before it is found not to be JSON.
func TestStreamReturnsEventsBeforeAnError(t *testing.T) {
	events, err := completions.New().NewStreamDecoder().Decode([]byte("not JSON"))
	if err == nil {
		t.Fatal("Decode = nil error, want one")
	}
	assertTrace(t, events, "0 response.created in_progress")
}

func TestEncodeStream(t *testing.T) {
	req := &protocol.Request{
		Model:  "m",
		Input:  []protocol.Item{text(protocol.RoleUser, "hi")},
		Native: jsontext.Value(`{"temperature":0}`),
	}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	header := make(http.Header)
	body, err := completions.New().EncodeStreamRequest(req, header)
	if err != nil {
		t.Fatalf("EncodeStreamRequest: %v", err)
	}
	if len(header) != 0 {
		t.Errorf("EncodeStreamRequest added headers %v, want none", header)
	}
	assertJSON(t, body, `{"model":"m","messages":[{"role":"user","content":"hi"}],
		"stream":true,"stream_options":{"include_usage":true},"temperature":0}`)
}

// TestEncodeRefusesNativeStream wants a Native stream or stream_options member refused as a
// clash, streamed or not.
func TestEncodeRefusesNativeStream(t *testing.T) {
	f := completions.New()
	encodings := map[string]func(*protocol.Request, http.Header) ([]byte, error){
		"EncodeRequest":       f.EncodeRequest,
		"EncodeStreamRequest": f.EncodeStreamRequest,
	}
	for name, encode := range encodings {
		for _, member := range []string{"stream", "stream_options"} {
			t.Run(name+"/"+member, func(t *testing.T) {
				body, err := encode(&protocol.Request{
					Model:  "m",
					Input:  []protocol.Item{text(protocol.RoleUser, "hi")},
					Native: jsontext.Value(`{"` + member + `":{}}`),
				}, make(http.Header))
				if err == nil {
					t.Fatalf("%s = %s, nil; want a clash error", name, body)
				}
				if want := "native member " + member + " clashes"; !strings.Contains(err.Error(), want) {
					t.Errorf("%s error = %q, want it to contain %q", name, err, want)
				}
			})
		}
	}
}
