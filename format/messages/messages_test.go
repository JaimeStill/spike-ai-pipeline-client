package messages_test

import (
	"encoding/json/jsontext"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/format/messages"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// encode encodes req, failing t if it is invalid or EncodeRequest refuses it, or unless
// EncodeRequest sets the anthropic-version header, 2026-10-21, and no other. The client validates
// every request before encoding it, so a request the format is given here is a valid one.
func encode(t *testing.T, req *protocol.Request) []byte {
	t.Helper()
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	header := make(http.Header)
	body, err := messages.New().EncodeRequest(req, header)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	want := http.Header{"Anthropic-Version": {"2026-10-21"}}
	if !reflect.DeepEqual(header, want) {
		t.Errorf("EncodeRequest headers = %v, want %v", header, want)
	}
	return body
}

// decode decodes reply as the body of a 2xx reply.
func decode(reply string) (*protocol.Response, error) {
	return messages.New().DecodeResponse([]byte(reply))
}

// readTestdata returns the file name in testdata.
func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// assertJSON fails t unless got and want are the same JSON, ignoring member order and spacing.
func assertJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	g, w := jsontext.Value(append([]byte(nil), got...)), jsontext.Value(want)
	if err := g.Canonicalize(); err != nil {
		t.Fatalf("body %s isn't JSON: %v", got, err)
	}
	if err := w.Canonicalize(); err != nil {
		t.Fatalf("want %s isn't JSON: %v", want, err)
	}
	if string(g) != string(w) {
		t.Errorf("body = %s\nwant   %s", g, w)
	}
}

func text(role protocol.Role, s string) protocol.Message {
	return protocol.Message{Role: role, Content: []protocol.ContentPart{protocol.InputText{Text: s}}}
}

func assistant(parts ...protocol.ContentPart) protocol.Message {
	return protocol.Message{Role: protocol.RoleAssistant, Content: parts}
}

// ask is a request to model m of the input items, bounded at 64 output tokens.
func ask(input ...protocol.Item) *protocol.Request {
	return &protocol.Request{Model: "m", Input: input, MaxOutputTokens: 64}
}

func TestRoute(t *testing.T) {
	if got := messages.New().Route(); got != "messages" {
		t.Errorf("Route = %q, want %q", got, "messages")
	}
}

func TestEncode(t *testing.T) {
	cases := []struct {
		name string
		req  *protocol.Request
		want string
	}{
		{
			name: "model, max_tokens, and a user message of one text block",
			req:  ask(text(protocol.RoleUser, "hi")),
			want: `{"model":"m","max_tokens":64,"messages":[
				{"role":"user","content":[{"type":"text","text":"hi"}]}]}`,
		},
		{
			name: "instructions as the top-level system",
			req: &protocol.Request{
				Model:           "m",
				Instructions:    "Answer in one word.",
				Input:           []protocol.Item{text(protocol.RoleUser, "Capital of France?")},
				MaxOutputTokens: 512,
			},
			want: `{"model":"m","max_tokens":512,"system":"Answer in one word.","messages":[
				{"role":"user","content":[{"type":"text","text":"Capital of France?"}]}]}`,
		},
		{
			name: "system and developer messages as system messages in their places",
			req: ask(
				text(protocol.RoleUser, "u"),
				text(protocol.RoleSystem, "s"),
				assistant(protocol.OutputText{Text: "a"}),
				text(protocol.RoleDeveloper, "d"),
				text(protocol.RoleUser, "u2"),
			),
			want: `{"model":"m","max_tokens":64,"messages":[
				{"role":"user","content":[{"type":"text","text":"u"}]},
				{"role":"system","content":[{"type":"text","text":"s"}]},
				{"role":"assistant","content":[{"type":"text","text":"a"}]},
				{"role":"system","content":[{"type":"text","text":"d"}]},
				{"role":"user","content":[{"type":"text","text":"u2"}]}]}`,
		},
		{
			name: "several text parts as several text blocks",
			req: ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				protocol.InputText{Text: "one"}, protocol.InputText{Text: "two"},
			}}),
			want: `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":[
				{"type":"text","text":"one"},{"type":"text","text":"two"}]}]}`,
		},
		{
			name: "assistant output_text and refusal as text blocks",
			req: ask(
				text(protocol.RoleUser, "q"),
				assistant(protocol.OutputText{Text: "Partly:"}, protocol.Refusal{Refusal: "not that"}),
				text(protocol.RoleUser, "ok"),
			),
			want: `{"model":"m","max_tokens":64,"messages":[
				{"role":"user","content":[{"type":"text","text":"q"}]},
				{"role":"assistant","content":[{"type":"text","text":"Partly:"},{"type":"text","text":"not that"}]},
				{"role":"user","content":[{"type":"text","text":"ok"}]}]}`,
		},
		{
			name: "image by data URL as a base64 source, its detail dropped",
			req: ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				protocol.InputText{Text: "Describe this image."},
				protocol.InputImage{
					ImageURL: protocol.DataURL("image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}),
					Detail:   protocol.ImageDetailLow,
				},
			}}),
			want: `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":[
				{"type":"text","text":"Describe this image."},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}}]}]}`,
		},
		{
			name: "image by data URL with a media type parameter, the type alone",
			req: ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				protocol.InputImage{ImageURL: "data:image/jpeg;name=a.jpg;base64,/9j/"},
			}}),
			want: `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":[
				{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"/9j/"}}]}]}`,
		},
		{
			name: "image by remote URL as a url source, its detail dropped",
			req: ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				protocol.InputImage{ImageURL: "https://example.com/shapes.png", Detail: protocol.ImageDetailHigh},
			}}),
			want: `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":[
				{"type":"image","source":{"type":"url","url":"https://example.com/shapes.png"}}]}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertJSON(t, encode(t, tc.req), tc.want)
		})
	}
}

// TestEncodeRefuses covers the valid requests the format refuses, each before it sets a header.
func TestEncodeRefuses(t *testing.T) {
	user := text(protocol.RoleUser, "hi")
	image := func(url string) *protocol.Request {
		return ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.InputImage{ImageURL: url}}})
	}
	cases := []struct {
		name string
		req  *protocol.Request
		want string
	}{
		{"no max output tokens", &protocol.Request{Model: "m", Input: []protocol.Item{user}},
			"request has no max output tokens, which Messages requires as max_tokens"},
		{"input_audio", ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
			protocol.InputText{Text: "Transcribe."},
			protocol.InputAudio{Data: []byte("RIFF"), Format: protocol.AudioFormatWAV},
		}}), "input 0: content 1: part protocol.InputAudio has no Messages form"},
		{"data URL not base64", image("data:image/png,%89PNG"), "input 0: content 0: image data URL isn't base64"},
		{"data URL of no media type", image("data:;base64,iVBORw0KGgo="), "image data URL has no media type"},
		{"data URL of no data", image("data:image/png;base64"), "image data URL has no data"},
		{"function_call", ask(user, protocol.FunctionCall{CallID: "c1", Name: "f", Arguments: "{}"}),
			"input 1: item protocol.FunctionCall is not yet supported"},
		{"tools", &protocol.Request{Model: "m", Input: []protocol.Item{user}, MaxOutputTokens: 64,
			Tools: []protocol.FunctionTool{{Name: "f"}}}, "tools are not yet supported"},
		{"reasoning effort", &protocol.Request{Model: "m", Input: []protocol.Item{user}, MaxOutputTokens: 64,
			Reasoning: protocol.ReasoningParam{Effort: protocol.ReasoningEffortLow}}, "a reasoning effort is not yet supported"},
		{"native", &protocol.Request{Model: "m", Input: []protocol.Item{user}, MaxOutputTokens: 64,
			Native: jsontext.Value(`{"temperature":0}`)}, "native members are not yet supported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			header := make(http.Header)
			body, err := messages.New().EncodeRequest(tc.req, header)
			if err == nil {
				t.Fatalf("EncodeRequest = %s, nil; want an error containing %q", body, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("EncodeRequest error = %q, want it to contain %q", err, tc.want)
			}
			if len(header) != 0 {
				t.Errorf("EncodeRequest set headers %v on a refused request, want none", header)
			}
		})
	}
}

func TestEncodeStreamRequestRefuses(t *testing.T) {
	body, err := messages.New().EncodeStreamRequest(ask(text(protocol.RoleUser, "hi")), make(http.Header))
	if err == nil || !strings.Contains(err.Error(), "streaming is not yet supported") {
		t.Errorf("EncodeStreamRequest = %s, %v; want streaming refused", body, err)
	}
}

// TestDecodeRecorded decodes the router's replies recorded in testdata.
func TestDecodeRecorded(t *testing.T) {
	cases := []struct {
		file string
		want *protocol.Response
	}{
		{
			// The thinking block ahead of the text is dropped, making no item.
			file: "text.json",
			want: &protocol.Response{
				Output: []protocol.Item{protocol.Message{
					ID:      "chatcmpl-AM4P3ZlmlUsqESx8ANKHI8limmMGUALi/0",
					Status:  protocol.StatusCompleted,
					Role:    protocol.RoleAssistant,
					Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris, Lyon, Marseille"}},
				}},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 91, OutputTokens: 85, TotalTokens: 176},
			},
		},
		{
			file: "length.json",
			want: &protocol.Response{
				Output: []protocol.Item{protocol.Message{
					ID:      "chatcmpl-RMGRO2IdNL6QNnOC6Yk1axTBpXQk3Zsk/0",
					Status:  protocol.StatusIncomplete,
					Role:    protocol.RoleAssistant,
					Content: []protocol.ContentPart{protocol.OutputText{Text: "1 2 3 4 "}},
				}},
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
				Usage:             protocol.Usage{InputTokens: 29, OutputTokens: 8, TotalTokens: 37},
			},
		},
		{
			file: "conversation.json",
			want: &protocol.Response{
				Output: []protocol.Item{protocol.Message{
					ID:      "chatcmpl-yCOWmZF11j1jz7AcJ32cFQgRD1KScyo0/0",
					Status:  protocol.StatusCompleted,
					Role:    protocol.RoleAssistant,
					Content: []protocol.ContentPart{protocol.OutputText{Text: "ROME"}},
				}},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 54, OutputTokens: 2, TotalTokens: 56},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			resp, err := messages.New().DecodeResponse(readTestdata(t, tc.file))
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("DecodeResponse = %#v\nwant            %#v", resp, tc.want)
			}
		})
	}
}

func TestDecode(t *testing.T) {
	msg := func(id string, status protocol.Status, texts ...string) protocol.Message {
		m := protocol.Message{ID: id, Status: status, Role: protocol.RoleAssistant}
		for _, s := range texts {
			m.Content = append(m.Content, protocol.OutputText{Text: s})
		}
		return m
	}
	cases := []struct {
		name  string
		reply string
		want  *protocol.Response
	}{
		{
			name: "consecutive text blocks as one message, a part each",
			reply: `{"id":"msg_1","type":"message","role":"assistant","content":[
				{"type":"text","text":"Paris"},{"type":"text","text":", Lyon"}],"stop_reason":"end_turn",
				"usage":{"input_tokens":10,"output_tokens":3}}`,
			want: &protocol.Response{
				Output: []protocol.Item{msg("msg_1/0", protocol.StatusCompleted, "Paris", ", Lyon")},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 10, OutputTokens: 3, TotalTokens: 13},
			},
		},
		{
			name: "runs of text split by thinking, only the last item incomplete",
			reply: `{"id":"msg_2","content":[
				{"type":"text","text":"one"},{"type":"thinking","thinking":"hm","signature":""},
				{"type":"text","text":"two"},{"type":"redacted_thinking","data":"x"},
				{"type":"text","text":"three"},{"type":"text","text":"four"}],"stop_reason":"max_tokens"}`,
			want: &protocol.Response{
				Output: []protocol.Item{
					msg("msg_2/0", protocol.StatusCompleted, "one"),
					msg("msg_2/1", protocol.StatusCompleted, "two"),
					msg("msg_2/2", protocol.StatusIncomplete, "three", "four"),
				},
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
			},
		},
		{
			name:  "no content gives no output",
			reply: `{"id":"msg_3","content":[],"stop_reason":"max_tokens"}`,
			want: &protocol.Response{
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
			},
		},
		{
			name:  "no id, so no item ids",
			reply: `{"content":[{"type":"text","text":"Paris"}],"stop_reason":"end_turn"}`,
			want: &protocol.Response{
				Output: []protocol.Item{msg("", protocol.StatusCompleted, "Paris")},
				Status: protocol.StatusCompleted,
			},
		},
		{
			name: "every input count in InputTokens",
			reply: `{"id":"msg_4","content":[{"type":"text","text":"Paris"}],"stop_reason":"end_turn",
				"usage":{"input_tokens":5,"cache_read_input_tokens":100,"cache_creation_input_tokens":20,"output_tokens":7}}`,
			want: &protocol.Response{
				Output: []protocol.Item{msg("msg_4/0", protocol.StatusCompleted, "Paris")},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 125, OutputTokens: 7, TotalTokens: 132},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := decode(tc.reply)
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("DecodeResponse = %#v\nwant            %#v", resp, tc.want)
			}
		})
	}
}

func TestDecodeStopReason(t *testing.T) {
	maxTokens := &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}
	cases := []struct {
		reason  string
		status  protocol.Status
		details *protocol.IncompleteDetails
	}{
		{"end_turn", protocol.StatusCompleted, nil},
		{"tool_use", protocol.StatusCompleted, nil},
		{"stop_sequence", protocol.StatusCompleted, nil},
		{"max_tokens", protocol.StatusIncomplete, maxTokens},
		{"model_context_window_exceeded", protocol.StatusIncomplete, maxTokens},
		{"refusal", protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonContentFilter}},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			resp, err := decode(`{"id":"msg_1","content":[{"type":"text","text":"Paris"}],"stop_reason":"` + tc.reason + `"}`)
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			if resp.Status != tc.status {
				t.Errorf("Status = %q, want %q", resp.Status, tc.status)
			}
			if !reflect.DeepEqual(resp.IncompleteDetails, tc.details) {
				t.Errorf("IncompleteDetails = %+v, want %+v", resp.IncompleteDetails, tc.details)
			}
			want := []protocol.Item{protocol.Message{
				ID:      "msg_1/0",
				Status:  tc.status,
				Role:    protocol.RoleAssistant,
				Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}},
			}}
			if !reflect.DeepEqual(resp.Output, want) {
				t.Errorf("Output = %#v, want %#v", resp.Output, want)
			}
		})
	}
}

func TestDecodeRefusesReply(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  string
	}{
		{"pause_turn", `{"content":[{"type":"text","text":"x"}],"stop_reason":"pause_turn"}`, `unknown stop_reason "pause_turn"`},
		{"unknown stop_reason", `{"content":[{"type":"text","text":"x"}],"stop_reason":"length"}`, `unknown stop_reason "length"`},
		{"null stop_reason", `{"content":[{"type":"text","text":"x"}],"stop_reason":null}`, `unknown stop_reason ""`},
		{"block of unknown type", `{"content":[{"type":"text","text":"x"},{"type":"tool_use","id":"t1","name":"f","input":{}}],
			"stop_reason":"tool_use"}`, `content block 1 has unknown type "tool_use"`},
		{"not JSON", `Paris`, "decode response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := decode(tc.reply)
			if err == nil {
				t.Fatalf("DecodeResponse = %v, nil; want an error containing %q", resp, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("DecodeResponse error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestDecodeError(t *testing.T) {
	cases := []struct {
		name               string
		body               string
		message, typ, code string
	}{
		{"llama.cpp's, recorded", string(readTestdata(t, "model_not_found.json")),
			"model 'spike-ai-pipeline/no-such-model' not found", "invalid_request_error", "400"},
		{"Anthropic's", `{"type":"error","error":{"type":"not_found_error","message":"model: claude-x"},"request_id":"req_1"}`,
			"model: claude-x", "not_found_error", ""},
		{"not JSON", "upstream down\n", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			message, typ, code := messages.New().DecodeError([]byte(tc.body))
			if message != tc.message || typ != tc.typ || code != tc.code {
				t.Errorf("DecodeError = {%q %q %q}, want {%q %q %q}",
					message, typ, code, tc.message, tc.typ, tc.code)
			}
		})
	}
}

func TestStreamDecoderRefuses(t *testing.T) {
	d := messages.New().NewStreamDecoder()
	if _, err := d.Decode([]byte(`{"type":"message_start"}`)); err == nil {
		t.Error("Decode = nil error, want streaming refused")
	}
	if _, err := d.End(); err == nil {
		t.Error("End = nil error, want streaming refused")
	}
}
