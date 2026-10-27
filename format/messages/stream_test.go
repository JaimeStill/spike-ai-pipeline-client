package messages_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/format/messages"
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
// the client does, the data alone, and returns the events it made, telling it the stream ended
// at the body's end. It stops at the first error.
func decodeStream(t *testing.T, body string) streamResult {
	t.Helper()
	decoder := messages.New().NewStreamDecoder()
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

// stream is an event stream of the events' data, each named for its type, as the router writes
// them, with a comment, the router's keep-alive, after the first.
func stream(events ...string) string {
	var b strings.Builder
	for i, data := range events {
		var e struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(data), &e)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", e.Type, data)
		if i == 0 {
			b.WriteString(":\n\n")
		}
	}
	return b.String()
}

// The events of a stream of reply msg_1, as Anthropic and the router send them.
const (
	// messageStart opens reply msg_1, of 7 input tokens, 2 read from the cache and 1 written to it.
	messageStart = `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"usage":{"input_tokens":4,"cache_read_input_tokens":2,"cache_creation_input_tokens":1,"output_tokens":1}}}`
	messageStop  = `{"type":"message_stop"}`
	ping         = `{"type":"ping"}`
)

// usageJSON is reply msg_1's usage, unstreamed, as messageStart and messageDelta give it.
const usageJSON = `"usage":{"input_tokens":4,"cache_read_input_tokens":2,"cache_creation_input_tokens":1,"output_tokens":5}`

// messageDelta is message_delta of stop_reason and 5 output tokens.
func messageDelta(stopReason string) string {
	return `{"type":"message_delta","delta":{"stop_reason":"` + stopReason + `","stop_sequence":null},"usage":{"output_tokens":5}}`
}

// blockStart is content_block_start of the block at index, block its JSON.
func blockStart(index int, block string) string {
	return fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":%s}`, index, block)
}

// blockDelta is content_block_delta of the block at index, of a delta of typ whose member of
// typ's carries s.
func blockDelta(index int, typ, s string) string {
	member := map[string]string{
		"text_delta":       "text",
		"thinking_delta":   "thinking",
		"signature_delta":  "signature",
		"input_json_delta": "partial_json",
	}[typ]
	if member == "" {
		member = "value"
	}
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":%q,%q:%s}}`, index, typ, member, quote(s))
}

// blockStop is content_block_stop of the block at index.
func blockStop(index int) string {
	return fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index)
}

// trace renders events one per line, each its spec type and the members it carries: a part
// event's output index, content index, and item id, then its part or text, and an arguments
// event's output index and item id, then its arguments text. A run of reasoning deltas, which a
// recorded stream makes by the dozen, is one line of its first and last sequence numbers and its
// count; the reasoning.done after it gives their text joined, which the Accumulator checks.
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

// asStreamed returns reply, an unstreamed reply's body, with the ids body, a text/event-stream
// reply to the same request, gave: the reply's, message_start's, and each tool_use block's, the
// one the stream's block in its place began with. The recorded replies were each sent twice, and
// the router makes new ids for each.
func asStreamed(t *testing.T, reply, body string) string {
	t.Helper()
	var r map[string]any
	if err := json.Unmarshal([]byte(reply), &r); err != nil {
		t.Fatal(err)
	}
	var callIDs []string
	events := sse.NewDecoder(strings.NewReader(body))
	for {
		e, err := events.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			Type    string `json:"type"`
			Message struct {
				ID string `json:"id"`
			} `json:"message"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(e.Data), &event); err != nil {
			t.Fatal(err)
		}
		switch {
		case event.Type == "message_start":
			r["id"] = event.Message.ID
		case event.Type == "content_block_start" && event.ContentBlock.Type == "tool_use":
			callIDs = append(callIDs, event.ContentBlock.ID)
		}
	}
	for _, b := range r["content"].([]any) {
		if b := b.(map[string]any); b["type"] == "tool_use" {
			if len(callIDs) == 0 {
				t.Fatal("the reply has more tool_use blocks than the stream")
			}
			b["id"], callIDs = callIDs[0], callIDs[1:]
		}
	}
	if len(callIDs) > 0 {
		t.Fatalf("the stream has %d tool_use blocks more than the reply", len(callIDs))
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// sameArguments sets each function call of want to the arguments of got's in its place, failing
// t unless the two are the same JSON value: llama.cpp's router parses a call's arguments and
// writes them back compact in an unstreamed reply's input, and streams the model's own text, its
// spacing included, as the input_json deltas.
func sameArguments(t *testing.T, got, want *protocol.Response) {
	t.Helper()
	if len(got.Output) != len(want.Output) {
		return
	}
	for i, it := range want.Output {
		w, ok := it.(protocol.FunctionCall)
		g, ok2 := got.Output[i].(protocol.FunctionCall)
		if !ok || !ok2 {
			continue
		}
		gv, wv := jsontext.Value(g.Arguments), jsontext.Value(w.Arguments)
		if gv.Canonicalize() != nil || wv.Canonicalize() != nil || string(gv) != string(wv) {
			t.Errorf("output %d's arguments %q, want the JSON of %q", i, g.Arguments, w.Arguments)
			continue
		}
		w.Arguments = g.Arguments
		want.Output[i] = w
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
			1 response.output_item.added 0 protocol.Reasoning{ID:"chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
			2 response.content_part.added 0 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/0 protocol.ReasoningText{Text:""}
			3-72 response.reasoning.delta 0 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/0, 70 deltas
			73 response.output_item.added 1 protocol.Message{ID:"chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/1", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
			74 response.content_part.added 1 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/1 protocol.OutputText{Text:""}
			75 response.output_text.delta 1 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/1 "Paris"
			76 response.output_text.delta 1 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/1 ","
			77 response.output_text.delta 1 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/1 " Lyon"
			78 response.output_text.delta 1 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/1 ","
			79 response.output_text.delta 1 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/1 " Marseille"
			80 response.reasoning.done 0 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/0 "The user asks: \"Name three French cities, comma-separated, nothing else.\" The developer says: \"Answer with the names alone.\" So we should output three French city names, separated by commas, no extra text. So something like \"Paris, Lyon, Marseille\". Ensure no extra punctuation beyond commas. Probably just \"Paris, Lyon, Marseille\"."
			81 response.content_part.done 0 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/0 protocol.ReasoningText{Text:"The user asks: \"Name three French cities, comma-separated, nothing else.\" The developer says: \"Answer with the names alone.\" So we should output three French city names, separated by commas, no extra text. So something like \"Paris, Lyon, Marseille\". Ensure no extra punctuation beyond commas. Probably just \"Paris, Lyon, Marseille\"."}
			82 response.output_item.done 0 protocol.Reasoning{ID:"chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"The user asks: \"Name three French cities, comma-separated, nothing else.\" The developer says: \"Answer with the names alone.\" So we should output three French city names, separated by commas, no extra text. So something like \"Paris, Lyon, Marseille\". Ensure no extra punctuation beyond commas. Probably just \"Paris, Lyon, Marseille\"."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:messages/v1:eyJ0aGlua2luZyI6IlRoZSB1c2VyIGFza3M6IFwiTmFtZSB0aHJlZSBGcmVuY2ggY2l0aWVzLCBjb21tYS1zZXBhcmF0ZWQsIG5vdGhpbmcgZWxzZS5cIiBUaGUgZGV2ZWxvcGVyIHNheXM6IFwiQW5zd2VyIHdpdGggdGhlIG5hbWVzIGFsb25lLlwiIFNvIHdlIHNob3VsZCBvdXRwdXQgdGhyZWUgRnJlbmNoIGNpdHkgbmFtZXMsIHNlcGFyYXRlZCBieSBjb21tYXMsIG5vIGV4dHJhIHRleHQuIFNvIHNvbWV0aGluZyBsaWtlIFwiUGFyaXMsIEx5b24sIE1hcnNlaWxsZVwiLiBFbnN1cmUgbm8gZXh0cmEgcHVuY3R1YXRpb24gYmV5b25kIGNvbW1hcy4gUHJvYmFibHkganVzdCBcIlBhcmlzLCBMeW9uLCBNYXJzZWlsbGVcIi4iLCJzaWduYXR1cmUiOiIifQ=="}
			83 response.output_text.done 1 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/1 "Paris, Lyon, Marseille"
			84 response.content_part.done 1 0 chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/1 protocol.OutputText{Text:"Paris, Lyon, Marseille"}
			85 response.output_item.done 1 protocol.Message{ID:"chatcmpl-xErD8bz0KWGCDq5HyFTxsjbf9cfoaptY/1", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Paris, Lyon, Marseille"}}}
			86 response.completed 2 items`},
		{"tool_use", `
			0 response.created in_progress
			1 response.output_item.added 0 protocol.Reasoning{ID:"chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
			2 response.content_part.added 0 0 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/0 protocol.ReasoningText{Text:""}
			3-34 response.reasoning.delta 0 0 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/0, 32 deltas
			35 response.output_item.added 1 protocol.FunctionCall{ID:"chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1", Status:"in_progress", CallID:"mv30SBf3CaOZOxSTpdvW2Po1UjcsN5qx", Name:"get_weather", Arguments:""}
			36 response.function_call_arguments.delta 1 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1 "{"
			37 response.function_call_arguments.delta 1 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1 "\n  \""
			38 response.function_call_arguments.delta 1 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1 "city"
			39 response.function_call_arguments.delta 1 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1 "\":"
			40 response.function_call_arguments.delta 1 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1 " \""
			41 response.function_call_arguments.delta 1 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1 "Paris"
			42 response.function_call_arguments.delta 1 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1 "\""
			43 response.function_call_arguments.delta 1 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1 "\n}"
			44 response.reasoning.done 0 0 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/0 "The user asks: \"What is the weather in Paris right now?\" We need to get current temperature in Paris. Use function get_weather with city \"Paris\"."
			45 response.content_part.done 0 0 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/0 protocol.ReasoningText{Text:"The user asks: \"What is the weather in Paris right now?\" We need to get current temperature in Paris. Use function get_weather with city \"Paris\"."}
			46 response.output_item.done 0 protocol.Reasoning{ID:"chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"The user asks: \"What is the weather in Paris right now?\" We need to get current temperature in Paris. Use function get_weather with city \"Paris\"."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:messages/v1:eyJ0aGlua2luZyI6IlRoZSB1c2VyIGFza3M6IFwiV2hhdCBpcyB0aGUgd2VhdGhlciBpbiBQYXJpcyByaWdodCBub3c/XCIgV2UgbmVlZCB0byBnZXQgY3VycmVudCB0ZW1wZXJhdHVyZSBpbiBQYXJpcy4gVXNlIGZ1bmN0aW9uIGdldF93ZWF0aGVyIHdpdGggY2l0eSBcIlBhcmlzXCIuIiwic2lnbmF0dXJlIjoiIn0="}
			47 response.function_call_arguments.done 1 chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1 "{\n  \"city\": \"Paris\"\n}"
			48 response.output_item.done 1 protocol.FunctionCall{ID:"chatcmpl-cronVaswqQzQeTcPSoiG5Q30hfe8ctyd/1", Status:"completed", CallID:"mv30SBf3CaOZOxSTpdvW2Po1UjcsN5qx", Name:"get_weather", Arguments:"{\n  \"city\": \"Paris\"\n}"}
			49 response.completed 2 items`},
		{"length", `
			0 response.created in_progress
			1 response.output_item.added 0 protocol.Message{ID:"chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
			2 response.content_part.added 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 protocol.OutputText{Text:""}
			3 response.output_text.delta 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 "1"
			4 response.output_text.delta 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 " "
			5 response.output_text.delta 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 "2"
			6 response.output_text.delta 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 " "
			7 response.output_text.delta 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 "3"
			8 response.output_text.delta 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 " "
			9 response.output_text.delta 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 "4"
			10 response.output_text.delta 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 " "
			11 response.output_text.done 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 "1 2 3 4 "
			12 response.content_part.done 0 0 chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0 protocol.OutputText{Text:"1 2 3 4 "}
			13 response.output_item.done 0 protocol.Message{ID:"chatcmpl-bHH8azja1qYW2uwHwdXLPJBJjTus9A7p/0", Status:"incomplete", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"1 2 3 4 "}}}
			14 response.incomplete 1 items`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := string(readTestdata(t, tc.name+".sse"))
			r := decodeStream(t, body)
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			assertTrace(t, r.events, tc.trace)
			want, err := decode(asStreamed(t, string(readTestdata(t, tc.name+".json")), body))
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			got := fold(t, r.events)
			sameArguments(t, got, want)
			assertResponse(t, got, want)
		})
	}
}

// TestStreamUsage wants the terminal Response's usage counted as DecodeResponse counts it: its
// input message_start's input_tokens with both cache counts added, its output message_delta's
// output_tokens, not message_start's, and its total their sum.
func TestStreamUsage(t *testing.T) {
	r := decodeStream(t, stream(messageStart, messageDelta("end_turn"), messageStop))
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	completed, ok := r.events[len(r.events)-1].(protocol.ResponseCompleted)
	if !ok {
		t.Fatalf("last event = %#v, want response.completed", r.events[len(r.events)-1])
	}
	want := protocol.Usage{InputTokens: 7, OutputTokens: 5, TotalTokens: 12}
	if completed.Response.Usage != want {
		t.Errorf("Usage = %+v, want %+v", completed.Response.Usage, want)
	}
}

// openAtOnce are the blocks of a reply of thinking, text, and a call, each begun, and deltas
// given, as the router sends them, every block's stop to follow.
var openAtOnce = []string{
	messageStart,
	blockStart(0, `{"type":"thinking","thinking":""}`),
	blockDelta(0, "thinking_delta", "Need"),
	blockDelta(0, "thinking_delta", " the tool."),
	blockStart(1, `{"type":"text","text":""}`),
	blockDelta(1, "text_delta", "Checking."),
	blockStart(2, `{"type":"tool_use","id":"toolu_1","name":"get_weather"}`),
	blockDelta(2, "input_json_delta", `{"city":`),
	blockDelta(2, "input_json_delta", `"Paris"}`),
	blockDelta(0, "signature_delta", "sig"),
}

// openAtOnceReply is the reply openAtOnce's stream makes, unstreamed.
const openAtOnceReply = `{"id":"msg_1","content":[
	{"type":"thinking","thinking":"Need the tool.","signature":"sig"},
	{"type":"text","text":"Checking."},
	{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Paris"}}],
	"stop_reason":"tool_use",` + usageJSON + `}`

// TestStreamOpenAtOnce covers blocks open at once, as the router streams every reply: each part
// closes at its own block's stop, whatever order the stops come in, each item's output_item.done
// follows once its blocks have stopped and an item follows it, the last item's at message_delta,
// and the stream folds to what DecodeResponse gives for the same reply. Anthropic's order, each
// block stopped before the next begins, folds the same.
func TestStreamOpenAtOnce(t *testing.T) {
	cases := []struct {
		name   string
		events []string
		trace  string
	}{
		{
			name:   "stopped in order, as the router stops them",
			events: append(slices.Clone(openAtOnce), blockStop(0), blockStop(1), blockStop(2), messageDelta("tool_use"), messageStop),
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Reasoning{ID:"msg_1/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
				2 response.content_part.added 0 0 msg_1/0 protocol.ReasoningText{Text:""}
				3-4 response.reasoning.delta 0 0 msg_1/0, 2 deltas
				5 response.output_item.added 1 protocol.Message{ID:"msg_1/1", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
				6 response.content_part.added 1 0 msg_1/1 protocol.OutputText{Text:""}
				7 response.output_text.delta 1 0 msg_1/1 "Checking."
				8 response.output_item.added 2 protocol.FunctionCall{ID:"msg_1/2", Status:"in_progress", CallID:"toolu_1", Name:"get_weather", Arguments:""}
				9 response.function_call_arguments.delta 2 msg_1/2 "{\"city\":"
				10 response.function_call_arguments.delta 2 msg_1/2 "\"Paris\"}"
				11 response.reasoning.done 0 0 msg_1/0 "Need the tool."
				12 response.content_part.done 0 0 msg_1/0 protocol.ReasoningText{Text:"Need the tool."}
				13 response.output_item.done 0 protocol.Reasoning{ID:"msg_1/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"Need the tool."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:messages/v1:eyJ0aGlua2luZyI6Ik5lZWQgdGhlIHRvb2wuIiwic2lnbmF0dXJlIjoic2lnIn0="}
				14 response.output_text.done 1 0 msg_1/1 "Checking."
				15 response.content_part.done 1 0 msg_1/1 protocol.OutputText{Text:"Checking."}
				16 response.output_item.done 1 protocol.Message{ID:"msg_1/1", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Checking."}}}
				17 response.function_call_arguments.done 2 msg_1/2 "{\"city\":\"Paris\"}"
				18 response.output_item.done 2 protocol.FunctionCall{ID:"msg_1/2", Status:"completed", CallID:"toolu_1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
				19 response.completed 3 items`,
		},
		{
			name:   "stopped in reverse order",
			events: append(slices.Clone(openAtOnce), blockStop(2), blockStop(1), blockStop(0), messageDelta("tool_use"), messageStop),
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Reasoning{ID:"msg_1/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
				2 response.content_part.added 0 0 msg_1/0 protocol.ReasoningText{Text:""}
				3-4 response.reasoning.delta 0 0 msg_1/0, 2 deltas
				5 response.output_item.added 1 protocol.Message{ID:"msg_1/1", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
				6 response.content_part.added 1 0 msg_1/1 protocol.OutputText{Text:""}
				7 response.output_text.delta 1 0 msg_1/1 "Checking."
				8 response.output_item.added 2 protocol.FunctionCall{ID:"msg_1/2", Status:"in_progress", CallID:"toolu_1", Name:"get_weather", Arguments:""}
				9 response.function_call_arguments.delta 2 msg_1/2 "{\"city\":"
				10 response.function_call_arguments.delta 2 msg_1/2 "\"Paris\"}"
				11 response.function_call_arguments.done 2 msg_1/2 "{\"city\":\"Paris\"}"
				12 response.output_text.done 1 0 msg_1/1 "Checking."
				13 response.content_part.done 1 0 msg_1/1 protocol.OutputText{Text:"Checking."}
				14 response.output_item.done 1 protocol.Message{ID:"msg_1/1", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Checking."}}}
				15 response.reasoning.done 0 0 msg_1/0 "Need the tool."
				16 response.content_part.done 0 0 msg_1/0 protocol.ReasoningText{Text:"Need the tool."}
				17 response.output_item.done 0 protocol.Reasoning{ID:"msg_1/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"Need the tool."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:messages/v1:eyJ0aGlua2luZyI6Ik5lZWQgdGhlIHRvb2wuIiwic2lnbmF0dXJlIjoic2lnIn0="}
				18 response.output_item.done 2 protocol.FunctionCall{ID:"msg_1/2", Status:"completed", CallID:"toolu_1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
				19 response.completed 3 items`,
		},
		{
			name: "each stopped before the next begins, as Anthropic stops them",
			events: []string{
				messageStart,
				blockStart(0, `{"type":"thinking","thinking":""}`),
				blockDelta(0, "thinking_delta", "Need"),
				blockDelta(0, "thinking_delta", " the tool."),
				blockDelta(0, "signature_delta", "sig"),
				blockStop(0),
				blockStart(1, `{"type":"text","text":""}`),
				blockDelta(1, "text_delta", "Checking."),
				blockStop(1),
				blockStart(2, `{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{}}`),
				blockDelta(2, "input_json_delta", `{"city":`),
				blockDelta(2, "input_json_delta", `"Paris"}`),
				blockStop(2),
				messageDelta("tool_use"),
				messageStop,
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Reasoning{ID:"msg_1/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
				2 response.content_part.added 0 0 msg_1/0 protocol.ReasoningText{Text:""}
				3-4 response.reasoning.delta 0 0 msg_1/0, 2 deltas
				5 response.reasoning.done 0 0 msg_1/0 "Need the tool."
				6 response.content_part.done 0 0 msg_1/0 protocol.ReasoningText{Text:"Need the tool."}
				7 response.output_item.done 0 protocol.Reasoning{ID:"msg_1/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"Need the tool."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:messages/v1:eyJ0aGlua2luZyI6Ik5lZWQgdGhlIHRvb2wuIiwic2lnbmF0dXJlIjoic2lnIn0="}
				8 response.output_item.added 1 protocol.Message{ID:"msg_1/1", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
				9 response.content_part.added 1 0 msg_1/1 protocol.OutputText{Text:""}
				10 response.output_text.delta 1 0 msg_1/1 "Checking."
				11 response.output_text.done 1 0 msg_1/1 "Checking."
				12 response.content_part.done 1 0 msg_1/1 protocol.OutputText{Text:"Checking."}
				13 response.output_item.done 1 protocol.Message{ID:"msg_1/1", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Checking."}}}
				14 response.output_item.added 2 protocol.FunctionCall{ID:"msg_1/2", Status:"in_progress", CallID:"toolu_1", Name:"get_weather", Arguments:""}
				15 response.function_call_arguments.delta 2 msg_1/2 "{\"city\":"
				16 response.function_call_arguments.delta 2 msg_1/2 "\"Paris\"}"
				17 response.function_call_arguments.done 2 msg_1/2 "{\"city\":\"Paris\"}"
				18 response.output_item.done 2 protocol.FunctionCall{ID:"msg_1/2", Status:"completed", CallID:"toolu_1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
				19 response.completed 3 items`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.events...))
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			assertTrace(t, r.events, tc.trace)
			want, err := decode(openAtOnceReply)
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			assertResponse(t, fold(t, r.events), want)
		})
	}
}

// TestStreamBlocks covers each kind of block, and folds each stream to what DecodeResponse gives
// for the same reply: consecutive text blocks parts of one message; the signature and the
// redacted data kept for the reasoning item's envelope; a call of no input_json_delta of the
// arguments {}; and the status of the last item the stop_reason's.
func TestStreamBlocks(t *testing.T) {
	cases := []struct {
		name   string
		events []string
		trace  string
		reply  string
	}{
		{
			name: "two text blocks, one message, cut off at the bound",
			events: []string{
				messageStart,
				blockStart(0, `{"type":"text","text":""}`),
				blockDelta(0, "text_delta", "Paris"),
				blockStop(0),
				blockStart(1, `{"type":"text","text":""}`),
				blockDelta(1, "text_delta", ""),
				blockDelta(1, "text_delta", "Lyon"),
				blockStop(1),
				messageDelta("max_tokens"),
				messageStop,
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Message{ID:"msg_1/0", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
				2 response.content_part.added 0 0 msg_1/0 protocol.OutputText{Text:""}
				3 response.output_text.delta 0 0 msg_1/0 "Paris"
				4 response.output_text.done 0 0 msg_1/0 "Paris"
				5 response.content_part.done 0 0 msg_1/0 protocol.OutputText{Text:"Paris"}
				6 response.content_part.added 0 1 msg_1/0 protocol.OutputText{Text:""}
				7 response.output_text.delta 0 1 msg_1/0 "Lyon"
				8 response.output_text.done 0 1 msg_1/0 "Lyon"
				9 response.content_part.done 0 1 msg_1/0 protocol.OutputText{Text:"Lyon"}
				10 response.output_item.done 0 protocol.Message{ID:"msg_1/0", Status:"incomplete", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Paris"}, protocol.OutputText{Text:"Lyon"}}}
				11 response.incomplete 1 items`,
			reply: `{"id":"msg_1","content":[{"type":"text","text":"Paris"},{"type":"text","text":"Lyon"}],"stop_reason":"max_tokens",` + usageJSON + `}`,
		},
		{
			name: "thinking, its signature kept for the envelope",
			events: []string{
				messageStart,
				blockStart(0, `{"type":"thinking","thinking":""}`),
				blockDelta(0, "thinking_delta", "hm"),
				blockDelta(0, "signature_delta", "sig"),
				blockStop(0),
				messageDelta("end_turn"),
				messageStop,
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Reasoning{ID:"msg_1/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
				2 response.content_part.added 0 0 msg_1/0 protocol.ReasoningText{Text:""}
				3 response.reasoning.delta 0 0 msg_1/0 "hm"
				4 response.reasoning.done 0 0 msg_1/0 "hm"
				5 response.content_part.done 0 0 msg_1/0 protocol.ReasoningText{Text:"hm"}
				6 response.output_item.done 0 protocol.Reasoning{ID:"msg_1/0", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"hm"}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:messages/v1:eyJ0aGlua2luZyI6ImhtIiwic2lnbmF0dXJlIjoic2lnIn0="}
				7 response.completed 1 items`,
			reply: `{"id":"msg_1","content":[{"type":"thinking","thinking":"hm","signature":"sig"}],"stop_reason":"end_turn",` + usageJSON + `}`,
		},
		{
			name: "redacted thinking, then text",
			events: []string{
				messageStart,
				blockStart(0, `{"type":"redacted_thinking","data":"x"}`),
				blockStop(0),
				blockStart(1, `{"type":"text","text":""}`),
				blockDelta(1, "text_delta", "Paris"),
				blockStop(1),
				messageDelta("end_turn"),
				messageStop,
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.Reasoning{ID:"msg_1/0", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
				2 response.output_item.done 0 protocol.Reasoning{ID:"msg_1/0", Status:"completed", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:"spike-ai-pipeline:messages/v1:eyJkYXRhIjoieCJ9"}
				3 response.output_item.added 1 protocol.Message{ID:"msg_1/1", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
				4 response.content_part.added 1 0 msg_1/1 protocol.OutputText{Text:""}
				5 response.output_text.delta 1 0 msg_1/1 "Paris"
				6 response.output_text.done 1 0 msg_1/1 "Paris"
				7 response.content_part.done 1 0 msg_1/1 protocol.OutputText{Text:"Paris"}
				8 response.output_item.done 1 protocol.Message{ID:"msg_1/1", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Paris"}}}
				9 response.completed 2 items`,
			reply: `{"id":"msg_1","content":[{"type":"redacted_thinking","data":"x"},{"type":"text","text":"Paris"}],"stop_reason":"end_turn",` + usageJSON + `}`,
		},
		{
			name: "a call of no input",
			events: []string{
				messageStart,
				blockStart(0, `{"type":"tool_use","id":"toolu_1","name":"get_time","input":{}}`),
				blockStop(0),
				messageDelta("tool_use"),
				messageStop,
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.FunctionCall{ID:"msg_1/0", Status:"in_progress", CallID:"toolu_1", Name:"get_time", Arguments:""}
				2 response.function_call_arguments.delta 0 msg_1/0 "{}"
				3 response.function_call_arguments.done 0 msg_1/0 "{}"
				4 response.output_item.done 0 protocol.FunctionCall{ID:"msg_1/0", Status:"completed", CallID:"toolu_1", Name:"get_time", Arguments:"{}"}
				5 response.completed 1 items`,
			reply: `{"id":"msg_1","content":[{"type":"tool_use","id":"toolu_1","name":"get_time","input":{}}],"stop_reason":"tool_use",` + usageJSON + `}`,
		},
		{
			name: "a call of no input, its start of no input member, as the router's",
			events: []string{
				messageStart,
				blockStart(0, `{"type":"tool_use","id":"toolu_1","name":"get_time"}`),
				blockStop(0),
				messageDelta("refusal"),
				messageStop,
			},
			trace: `
				0 response.created in_progress
				1 response.output_item.added 0 protocol.FunctionCall{ID:"msg_1/0", Status:"in_progress", CallID:"toolu_1", Name:"get_time", Arguments:""}
				2 response.function_call_arguments.delta 0 msg_1/0 "{}"
				3 response.function_call_arguments.done 0 msg_1/0 "{}"
				4 response.output_item.done 0 protocol.FunctionCall{ID:"msg_1/0", Status:"incomplete", CallID:"toolu_1", Name:"get_time", Arguments:"{}"}
				5 response.incomplete 1 items`,
			reply: `{"id":"msg_1","content":[{"type":"tool_use","id":"toolu_1","name":"get_time","input":{}}],"stop_reason":"refusal",` + usageJSON + `}`,
		},
		{
			name:   "no content",
			events: []string{messageStart, messageDelta("end_turn"), messageStop},
			trace: `
				0 response.created in_progress
				1 response.completed 0 items`,
			reply: `{"id":"msg_1","content":[],"stop_reason":"end_turn",` + usageJSON + `}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.events...))
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

// TestStreamEnvelopes wants the reasoning items' output_item.done to carry the envelopes
// DecodeResponse writes, the thinking block's signature folded into its envelope, and each to go
// back out as the block it came as.
func TestStreamEnvelopes(t *testing.T) {
	r := decodeStream(t, stream(
		messageStart,
		blockStart(0, `{"type":"thinking","thinking":""}`),
		blockDelta(0, "thinking_delta", "h"),
		blockDelta(0, "thinking_delta", "m"),
		blockStart(1, `{"type":"redacted_thinking","data":"x"}`),
		blockStart(2, `{"type":"text","text":""}`),
		blockDelta(2, "text_delta", "Paris"),
		blockDelta(0, "signature_delta", "sig"),
		blockStop(0), blockStop(1), blockStop(2),
		messageDelta("end_turn"), messageStop,
	))
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	var envelopes []string
	for _, e := range r.events {
		if d, ok := e.(protocol.OutputItemDone); ok {
			if reasoning, ok := d.Item.(protocol.Reasoning); ok {
				envelopes = append(envelopes, reasoning.EncryptedContent)
			}
		}
	}
	if want := []string{thinkingEnvelope, redactedEnvelope}; !slices.Equal(envelopes, want) {
		t.Errorf("reasoning envelopes = %q, want %q", envelopes, want)
	}
	assertJSON(t, encode(t, ask(fold(t, r.events).Output...)), `{"model":"m","max_tokens":64,"stream":false,"messages":[
		{"role":"assistant","content":[
			{"type":"thinking","thinking":"hm","signature":"sig"},
			{"type":"redacted_thinking","data":"x"},
			{"type":"text","text":"Paris"}]}]}`)
}

// TestStreamIgnores wants ping events, events of a type the decoder doesn't know, and deltas of
// a type it doesn't know to make nothing: the stream makes the events it makes without them.
func TestStreamIgnores(t *testing.T) {
	events := []string{
		messageStart,
		blockStart(0, `{"type":"text","text":""}`),
		blockDelta(0, "text_delta", "Paris"),
		blockStop(0),
		messageDelta("end_turn"),
		messageStop,
	}
	want := decodeStream(t, stream(events...))
	if want.err != nil {
		t.Fatalf("stream error: %v", want.err)
	}
	cases := []struct {
		name  string
		extra string
	}{
		{"ping", ping},
		{"an unknown event", `{"type":"content_block_flourish","index":0}`},
		{"an unknown delta", blockDelta(0, "citations_delta", "a citation")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The extra event goes in the open text block, before its delta and after it.
			withExtra := slices.Concat(events[:2], []string{tc.extra}, events[2:3], []string{tc.extra}, events[3:])
			got := decodeStream(t, stream(withExtra...))
			if got.err != nil {
				t.Fatalf("stream error: %v", got.err)
			}
			if trace(got.events) != trace(want.events) {
				t.Errorf("events:\n%s\nwant those without it:\n%s", trace(got.events), trace(want.events))
			}
		})
	}
	// A ping ahead of message_start makes nothing either: response.created waits for it.
	decoder := messages.New().NewStreamDecoder()
	if events, err := decoder.Decode([]byte(ping)); err != nil || len(events) > 0 {
		t.Errorf("Decode(ping) first = %v, %v; want nothing", events, err)
	}
}

// TestStreamStopReason covers message_delta's stop_reason: the terminal event and the last
// item's status it gives, as DecodeResponse maps it, and pause_turn and an unknown one refused.
func TestStreamStopReason(t *testing.T) {
	text := []string{
		messageStart,
		blockStart(0, `{"type":"text","text":""}`),
		blockDelta(0, "text_delta", "Paris"),
		blockStop(0),
	}
	cases := []struct {
		reason  string
		status  protocol.Status
		details *protocol.IncompleteDetails
		err     string
	}{
		{reason: "end_turn", status: protocol.StatusCompleted},
		{reason: "stop_sequence", status: protocol.StatusCompleted},
		{reason: "tool_use", status: protocol.StatusCompleted},
		{reason: "max_tokens", status: protocol.StatusIncomplete, details: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}},
		{reason: "model_context_window_exceeded", status: protocol.StatusIncomplete, details: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}},
		{reason: "refusal", status: protocol.StatusIncomplete, details: &protocol.IncompleteDetails{Reason: protocol.ReasonContentFilter}},
		{reason: "pause_turn", err: `unknown stop_reason "pause_turn"`},
		{reason: "tired", err: `unknown stop_reason "tired"`},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			r := decodeStream(t, stream(append(slices.Clone(text), messageDelta(tc.reason), messageStop)...))
			if tc.err != "" {
				if r.err == nil || !strings.Contains(r.err.Error(), tc.err) {
					t.Fatalf("stream error = %v, want one containing %q", r.err, tc.err)
				}
				return
			}
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			resp := fold(t, r.events)
			last := r.events[len(r.events)-1]
			_, completed := last.(protocol.ResponseCompleted)
			_, incomplete := last.(protocol.ResponseIncomplete)
			if completed != (tc.status == protocol.StatusCompleted) || incomplete != (tc.status == protocol.StatusIncomplete) {
				t.Errorf("terminal event = %T, want the one of %s", last, tc.status)
			}
			if resp.Status != tc.status || !reflect.DeepEqual(resp.IncompleteDetails, tc.details) {
				t.Errorf("Response status %s, details %+v; want %s, %+v", resp.Status, resp.IncompleteDetails, tc.status, tc.details)
			}
			if m := resp.Output[0].(protocol.Message); m.Status != tc.status {
				t.Errorf("message status %s, want %s", m.Status, tc.status)
			}
		})
	}
}

// TestStreamErrorFrame wants an error frame, in either shape, recognized from its data alone, to
// close the stream as the spec has it: the error event of the frame's type, code, and message,
// then response.failed, its response failed, its error the frame's code and message, holding the
// output made before the frame, the items open at it in progress with what they had, which is
// what the Accumulator held just before the frame. An event after it is an error, and End makes
// nothing.
func TestStreamErrorFrame(t *testing.T) {
	const (
		anthropic = `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
		llama     = `{"code":500,"message":"context shift is disabled","type":"server_error"}`
	)
	overloaded := protocol.ErrorPayload{Type: "overloaded_error", Message: "Overloaded"}
	contextShift := protocol.ErrorPayload{Type: "server_error", Code: "500", Message: "context shift is disabled"}
	// openItems is every kind of item open: the thinking block stopped, its item waiting on the
	// stop_reason; the text block open; and the call open.
	openItems := slices.Concat(openAtOnce[:9], []string{blockStop(0)})
	openOutput := []protocol.Item{
		protocol.Reasoning{ID: "msg_1/0", Status: protocol.StatusCompleted, Content: []protocol.ReasoningText{{Text: "Need the tool."}}, EncryptedContent: seal(`{"thinking":"Need the tool.","signature":""}`)},
		protocol.Message{ID: "msg_1/1", Status: protocol.StatusInProgress, Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Checking."}}},
		protocol.FunctionCall{ID: "msg_1/2", Status: protocol.StatusInProgress, CallID: "toolu_1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
	}
	usage := protocol.Usage{InputTokens: 7, TotalTokens: 7}
	cases := []struct {
		name    string
		events  []string
		payload protocol.ErrorPayload
		output  []protocol.Item
		usage   protocol.Usage
		trace   string
	}{
		{name: "Anthropic's, first", events: []string{anthropic}, payload: overloaded, trace: `
			0 response.created in_progress
			1 error protocol.ErrorPayload{Type:"overloaded_error", Code:"", Message:"Overloaded", Param:""}
			2 response.failed 0 items`},
		{name: "llama.cpp's, first", events: []string{llama}, payload: contextShift, trace: `
			0 response.created in_progress
			1 error protocol.ErrorPayload{Type:"server_error", Code:"500", Message:"context shift is disabled", Param:""}
			2 response.failed 0 items`},
		{name: "Anthropic's, with items open", events: slices.Concat(openItems, []string{anthropic}), payload: overloaded, output: openOutput, usage: usage},
		{name: "llama.cpp's, with items open", events: slices.Concat(openItems, []string{llama}), payload: contextShift, output: openOutput, usage: usage},
		{name: "llama.cpp's, with every block stopped, waiting on message_delta", payload: contextShift, usage: usage,
			events: slices.Concat(openAtOnce, []string{blockStop(0), blockStop(1), blockStop(2), llama}),
			output: []protocol.Item{
				protocol.Reasoning{ID: "msg_1/0", Status: protocol.StatusCompleted, Content: []protocol.ReasoningText{{Text: "Need the tool."}}, EncryptedContent: seal(`{"thinking":"Need the tool.","signature":"sig"}`)},
				protocol.Message{ID: "msg_1/1", Status: protocol.StatusCompleted, Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Checking."}}},
				protocol.FunctionCall{ID: "msg_1/2", Status: protocol.StatusInProgress, CallID: "toolu_1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
			}},
		{name: "Anthropic's, of a redacted block open", payload: overloaded, usage: usage,
			events: []string{messageStart, blockStart(0, `{"type":"redacted_thinking","data":"x"}`), anthropic},
			output: []protocol.Item{protocol.Reasoning{ID: "msg_1/0", Status: protocol.StatusInProgress}}},
		{name: "Anthropic's, after message_delta", payload: overloaded,
			events: []string{messageStart, blockStart(0, `{"type":"text","text":""}`), blockDelta(0, "text_delta", "Paris"), blockStop(0), messageDelta("end_turn"), anthropic},
			output: []protocol.Item{protocol.Message{ID: "msg_1/0", Status: protocol.StatusCompleted, Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}}}},
			usage:  protocol.Usage{InputTokens: 7, OutputTokens: 5, TotalTokens: 12}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.events...))
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
			if before := fold(t, r.events[:n-2]); !reflect.DeepEqual(before.Output, tc.output) {
				t.Errorf("output before the frame = %#v\nwant the failed response's %#v", before.Output, tc.output)
			}

			decoder := messages.New().NewStreamDecoder()
			for _, data := range tc.events {
				if _, err := decoder.Decode([]byte(data)); err != nil {
					t.Fatalf("Decode: %v", err)
				}
			}
			if events, err := decoder.Decode([]byte(messageStop)); err == nil || len(events) > 0 {
				t.Errorf("Decode after the error frame = %v, %v; want no events and the stream ended", events, err)
			}
			if events, err := decoder.End(); err != nil || len(events) > 0 {
				t.Errorf("End after the error frame = %v, %v; want nothing", events, err)
			}
		})
	}
}

// TestStreamNotAnErrorFrame wants a body of a Messages event's type, or of no string message,
// read as no error frame: message_start's message is an object, and an unknown event with a
// message that isn't a string is ignored.
func TestStreamNotAnErrorFrame(t *testing.T) {
	r := decodeStream(t, stream(messageStart, `{"type":"content_block_flourish","message":{"text":"hi"}}`, messageDelta("end_turn"), messageStop))
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	if _, ok := r.events[len(r.events)-1].(protocol.ResponseCompleted); !ok {
		t.Errorf("last event = %#v, want response.completed", r.events[len(r.events)-1])
	}
}

// TestStreamEnd covers how a stream ends: at message_stop, and io.ErrUnexpectedEOF at an end
// before it, and End makes nothing once message_stop has made the terminal event.
func TestStreamEnd(t *testing.T) {
	cases := []struct {
		name     string
		events   []string
		complete bool
	}{
		{"message_stop", []string{messageStart, messageDelta("end_turn"), messageStop}, true},
		{"end after message_delta", []string{messageStart, messageDelta("end_turn")}, false},
		{"end with a block open", []string{messageStart, blockStart(0, `{"type":"text","text":""}`), blockDelta(0, "text_delta", "Par")}, false},
		{"end after message_start", []string{messageStart}, false},
		{"empty stream", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.events...))
			if !tc.complete {
				if !errors.Is(r.err, io.ErrUnexpectedEOF) {
					t.Errorf("stream error = %v, want io.ErrUnexpectedEOF", r.err)
				}
				return
			}
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			if _, ok := r.events[len(r.events)-1].(protocol.ResponseCompleted); !ok {
				t.Errorf("last event = %#v, want response.completed", r.events[len(r.events)-1])
			}
		})
	}
	decoder := messages.New().NewStreamDecoder()
	for _, data := range []string{messageStart, messageDelta("end_turn"), messageStop} {
		if _, err := decoder.Decode([]byte(data)); err != nil {
			t.Fatalf("Decode(%s): %v", data, err)
		}
	}
	if events, err := decoder.End(); events != nil || err != nil {
		t.Errorf("End = %v, %v; want nothing", events, err)
	}
}

func TestStreamRefusesEvent(t *testing.T) {
	text := blockStart(0, `{"type":"text","text":""}`)
	cases := []struct {
		name   string
		events []string
		want   string
	}{
		{"not JSON", []string{messageStart, `{"type":`}, "decode event"},
		{"a block before message_start", []string{text}, "content_block_start before message_start"},
		{"message_stop before message_start", []string{messageStop}, "message_stop before message_start"},
		{"a second message_start", []string{messageStart, messageStart}, "a second message_start"},
		{"a block of an unknown type", []string{messageStart, blockStart(0, `{"type":"server_tool_use","id":"s1"}`)}, `content block 0 has unknown type "server_tool_use"`},
		{"a block begun twice", []string{messageStart, text, text}, "content block 0 began twice"},
		{"a delta of a block not begun", []string{messageStart, blockDelta(3, "text_delta", "hi")}, "content block 3 didn't begin"},
		{"a delta of a block stopped", []string{messageStart, text, blockStop(0), blockDelta(0, "text_delta", "hi")}, "content block 0 has stopped"},
		{"a stop of a block not begun", []string{messageStart, blockStop(0)}, "content block 0 didn't begin"},
		{"a second stop", []string{messageStart, text, blockStop(0), blockStop(0)}, "content block 0 has stopped"},
		{"a delta of another block's type", []string{messageStart, text, blockDelta(0, "input_json_delta", "{}")}, "input_json_delta of content block 0, a text block"},
		{"a signature of a text block", []string{messageStart, text, blockDelta(0, "signature_delta", "sig")}, "signature_delta of content block 0, a text block"},
		{"a second text block with the first open", []string{messageStart, text, blockStart(1, `{"type":"text","text":""}`)}, "text block 1 began while the message's text block before it is open"},
		{"message_delta with a block open", []string{messageStart, text, messageDelta("end_turn")}, "message_delta while a content block of output 0 is open"},
		{"a block after message_delta", []string{messageStart, messageDelta("end_turn"), text}, "content block 0 began after message_delta"},
		{"a second message_delta", []string{messageStart, messageDelta("end_turn"), messageDelta("end_turn")}, "a second message_delta"},
		{"message_stop before message_delta", []string{messageStart, messageStop}, "message_stop before message_delta"},
		{"an event after message_stop", []string{messageStart, messageDelta("end_turn"), messageStop, ping}, "event after the end of the stream"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoder := messages.New().NewStreamDecoder()
			var err error
			for _, data := range tc.events {
				if _, err = decoder.Decode([]byte(data)); err != nil {
					break
				}
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Decode error = %v, want one containing %q", err, tc.want)
			}
			if _, err := decoder.Decode([]byte(ping)); err == nil {
				t.Error("Decode after an error = nil, want the stream broken")
			}
		})
	}
}

// TestStreamDecodeErrorIsNoEndOfStream wants an event cut short, whose JSON error wraps
// io.ErrUnexpectedEOF, reported as an event the format can't decode, not as a stream that ended
// early.
func TestStreamDecodeErrorIsNoEndOfStream(t *testing.T) {
	_, err := messages.New().NewStreamDecoder().Decode([]byte(`{"type":`))
	if err == nil || !strings.Contains(err.Error(), "decode event") || errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Decode error = %v, want a decode error that doesn't match io.ErrUnexpectedEOF", err)
	}
}

func TestEncodeStream(t *testing.T) {
	req := withNative(ask(text(protocol.RoleUser, "hi")), `{"temperature":0}`)
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	header := make(http.Header)
	body, err := messages.New().EncodeStreamRequest(req, header)
	if err != nil {
		t.Fatalf("EncodeStreamRequest: %v", err)
	}
	if want := (http.Header{"Anthropic-Version": {"2026-10-21"}}); !reflect.DeepEqual(header, want) {
		t.Errorf("EncodeStreamRequest headers = %v, want %v", header, want)
	}
	assertJSON(t, body, `{"model":"m","max_tokens":64,"stream":true,"temperature":0,
		"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
}

// TestEncodeStreamRefuses wants EncodeStreamRequest to refuse what EncodeRequest refuses, a
// request of no max output tokens among them, and a Native stream member, which clashes with the
// stream member the body always sets, streamed or not; each before it sets a header.
func TestEncodeStreamRefuses(t *testing.T) {
	f := messages.New()
	encodings := map[string]func(*protocol.Request, http.Header) ([]byte, error){
		"EncodeRequest":       f.EncodeRequest,
		"EncodeStreamRequest": f.EncodeStreamRequest,
	}
	cases := []struct {
		name string
		req  *protocol.Request
		want string
	}{
		{"no max output tokens", &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")}},
			"request has no max output tokens, which Messages requires as max_tokens"},
		{"native stream", withNative(ask(text(protocol.RoleUser, "hi")), `{"stream":false}`),
			"native member stream clashes with a member the request sets"},
	}
	for name, encode := range encodings {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				header := make(http.Header)
				body, err := encode(tc.req, header)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("%s = %s, %v; want an error containing %q", name, body, err, tc.want)
				}
				if len(header) != 0 {
					t.Errorf("%s set headers %v on a refused request, want none", name, header)
				}
			})
		}
	}
}
