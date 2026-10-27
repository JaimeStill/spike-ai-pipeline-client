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
// event's output index and item id, then its arguments text.
func trace(events []protocol.Event) string {
	var b strings.Builder
	for _, e := range events {
		switch e := e.(type) {
		case protocol.ResponseCreated:
			fmt.Fprintf(&b, "%d response.created %s\n", e.SequenceNumber, e.Response.Status)
		case protocol.ResponseCompleted:
			fmt.Fprintf(&b, "%d response.completed %d items\n", e.SequenceNumber, len(e.Response.Output))
		case protocol.ResponseIncomplete:
			fmt.Fprintf(&b, "%d response.incomplete %d items\n", e.SequenceNumber, len(e.Response.Output))
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
	return b.String()
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
// reply, gave: its id the stream's first chunk's, each tool call's id the one the stream gave the
// call in its place, and its reasoning_content left out, as the stream decoder translates the
// message and its calls alone. The recorded replies were each sent twice, and the router makes
// new ids for each, so the streamed reply's ids aren't the other's.
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
		delete(m, "reasoning_content")
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
// order their first fragments arrive: each fragment that carries one.
func streamCallIDs(t *testing.T, body string) []string {
	t.Helper()
	var ids []string
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
						ID string `json:"id"`
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
					ids = append(ids, call.ID)
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
			1 response.output_item.added 0 protocol.Message{ID:"chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
			2 response.content_part.added 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 protocol.OutputText{Text:""}
			3 response.output_text.delta 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 "Paris"
			4 response.output_text.delta 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 ","
			5 response.output_text.delta 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 " Lyon"
			6 response.output_text.delta 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 ","
			7 response.output_text.delta 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 " Marseille"
			8 response.output_text.done 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 "Paris, Lyon, Marseille"
			9 response.content_part.done 0 0 chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0 protocol.OutputText{Text:"Paris, Lyon, Marseille"}
			10 response.output_item.done 0 protocol.Message{ID:"chatcmpl-NL7ZS1BCe0E5im0oLhUHyZ2JRMCqBgq7/0", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Paris, Lyon, Marseille"}}}
			11 response.completed 1 items`},
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
			1 response.output_item.added 0 protocol.FunctionCall{ID:"chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0", Status:"in_progress", CallID:"LsrzvhpPKdbtJyriZMr6ivjrWBgmWA3j", Name:"get_weather", Arguments:""}
			2 response.function_call_arguments.delta 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 "{"
			3 response.function_call_arguments.delta 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 "\n  \""
			4 response.function_call_arguments.delta 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 "city"
			5 response.function_call_arguments.delta 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 "\":"
			6 response.function_call_arguments.delta 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 " \""
			7 response.function_call_arguments.delta 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 "Paris"
			8 response.function_call_arguments.delta 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 "\""
			9 response.function_call_arguments.delta 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 "\n}"
			10 response.function_call_arguments.done 0 chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0 "{\n  \"city\": \"Paris\"\n}"
			11 response.output_item.done 0 protocol.FunctionCall{ID:"chatcmpl-fbwrSmHus4NqdExvtVzLfY0BV9kduppP/0", Status:"completed", CallID:"LsrzvhpPKdbtJyriZMr6ivjrWBgmWA3j", Name:"get_weather", Arguments:"{\n  \"city\": \"Paris\"\n}"}
			12 response.completed 1 items`},
		{"parallel_tool_calls", `
			0 response.created in_progress
			1 response.output_item.added 0 protocol.FunctionCall{ID:"chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0", Status:"in_progress", CallID:"zzwGjyJxRF0NLM9cl3nvl9X1uykH0HJn", Name:"get_weather", Arguments:""}
			2 response.function_call_arguments.delta 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 "{"
			3 response.function_call_arguments.delta 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 "\"city"
			4 response.function_call_arguments.delta 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 "\":"
			5 response.function_call_arguments.delta 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 "\""
			6 response.function_call_arguments.delta 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 "Paris"
			7 response.function_call_arguments.delta 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 "\""
			8 response.function_call_arguments.delta 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 "}"
			9 response.function_call_arguments.done 0 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0 "{\"city\":\"Paris\"}"
			10 response.output_item.done 0 protocol.FunctionCall{ID:"chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/0", Status:"completed", CallID:"zzwGjyJxRF0NLM9cl3nvl9X1uykH0HJn", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
			11 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1", Status:"in_progress", CallID:"EXc5ifcsLJtON5nVlTOtEvXtYSHnWbgi", Name:"get_weather", Arguments:""}
			12 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "{"
			13 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "\"city"
			14 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "\":"
			15 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "\""
			16 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "Tokyo"
			17 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "\""
			18 response.function_call_arguments.delta 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "}"
			19 response.function_call_arguments.done 1 chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1 "{\"city\":\"Tokyo\"}"
			20 response.output_item.done 1 protocol.FunctionCall{ID:"chatcmpl-SDOdqBEtCPhaKm5xDbwNCDkOz758MHkS/1", Status:"completed", CallID:"EXc5ifcsLJtON5nVlTOtEvXtYSHnWbgi", Name:"get_weather", Arguments:"{\"city\":\"Tokyo\"}"}
			21 response.completed 2 items`},
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

// TestStreamToolCalls covers function calls: a call closes the message ahead of it, completed;
// each call closes the one ahead of it, completed; the call open at the finish_reason takes the
// status it gives; and each folds to what DecodeResponse gives for the same reply.
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
				3 response.function_call_arguments.done 0 chatcmpl-1/0 "{\"city\":\"Paris\"}"
				4 response.output_item.done 0 protocol.FunctionCall{ID:"chatcmpl-1/0", Status:"completed", CallID:"c1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
				5 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-1/1", Status:"in_progress", CallID:"c2", Name:"get_weather", Arguments:""}
				6 response.function_call_arguments.delta 1 chatcmpl-1/1 "{\"ci"
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
		{"a fragment of a closed call", []string{delta(callDelta(0, "c1", "get_weather", "{"), ""), delta(callDelta(1, "c2", "get_time", "{}"), ""), delta(callDelta(0, "", "", "}"), "")}, "a fragment of tool call 0 after tool call 1 began"},
		{"a first fragment of no name", []string{delta(callDelta(0, "c1", "", "{}"), "")}, "tool call 0's first fragment has no function name"},
		{"a first fragment of another type", []string{delta(strings.Replace(callDelta(0, "c1", "get_weather", "{}"), `"function"`, `"custom"`, 1), "")}, `tool call 0 has unknown type "custom"`},
		{"a fragment naming another call", []string{delta(callDelta(0, "c1", "get_weather", "{"), ""), delta(callDelta(0, "c2", "get_weather", "}"), "")}, `a fragment of tool call 0 names call "c2"`},
		{"message text after a call", []string{delta(callDelta(0, "c1", "get_weather", "{}"), ""), text}, "message text after a tool call"},
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
