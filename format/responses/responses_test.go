package responses_test

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/format/responses"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// encode encodes req with the spec's form of the format, as encodeWith does.
func encode(t *testing.T, req *protocol.Request) []byte {
	t.Helper()
	return encodeWith(t, responses.Options{}, req)
}

// encodeWith encodes req with the format adjusted by opts, failing t if it is invalid or
// EncodeRequest refuses it, or if EncodeRequest sets a header. The client validates every
// request before encoding it, so a request the format is given here is a valid one.
func encodeWith(t *testing.T, opts responses.Options, req *protocol.Request) []byte {
	t.Helper()
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	header := make(http.Header)
	body, err := responses.New(opts).EncodeRequest(req, header)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	if len(header) != 0 {
		t.Errorf("EncodeRequest set headers %v, want none", header)
	}
	return body
}

// decode decodes reply as the body of a 2xx reply.
func decode(reply string) (*protocol.Response, error) {
	return responses.New(responses.Options{}).DecodeResponse([]byte(reply))
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

// seal returns the responses format's envelope of the server's encrypted_content enc and the
// reasoning texts, as DecodeResponse writes it into a reasoning item's encrypted_content.
func seal(t *testing.T, enc string, texts ...string) string {
	t.Helper()
	if texts == nil {
		texts = []string{}
	}
	b, err := json.Marshal(struct {
		EncryptedContent string   `json:"encrypted_content"`
		Content          []string `json:"content"`
	}{enc, texts})
	if err != nil {
		t.Fatal(err)
	}
	return "spike-ai-pipeline:responses/v1:" + base64.StdEncoding.EncodeToString(b)
}

func text(role protocol.Role, s string) protocol.Message {
	return protocol.Message{Role: role, Content: []protocol.ContentPart{protocol.InputText{Text: s}}}
}

// withNative returns req with native as its Native.
func withNative(req *protocol.Request, native string) *protocol.Request {
	req.Native = jsontext.Value(native)
	return req
}

// ask is a request to model m of the input items, with no bound.
func ask(input ...protocol.Item) *protocol.Request {
	return &protocol.Request{Model: "m", Input: input}
}

// stateless is the members every request carries, after its own.
const stateless = `"stream":false,"store":false,"include":["reasoning.encrypted_content"]`

func TestEncode(t *testing.T) {
	cases := []struct {
		name string
		req  *protocol.Request
		want string
	}{
		{
			name: "model and a user message of one input_text, no bound sent",
			req:  ask(text(protocol.RoleUser, "hi")),
			want: `{"model":"m",` + stateless + `,"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`,
		},
		{
			name: "instructions and max_output_tokens",
			req: &protocol.Request{
				Model:           "m",
				Instructions:    "Answer in one word.",
				Input:           []protocol.Item{text(protocol.RoleUser, "Capital of France?")},
				MaxOutputTokens: 512,
			},
			want: `{"model":"m","instructions":"Answer in one word.","max_output_tokens":512,` + stateless + `,"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"Capital of France?"}]}]}`,
		},
		{
			name: "every role in its place, output_text and refusal, an item's id, its status not sent",
			req: ask(
				text(protocol.RoleSystem, "s"),
				text(protocol.RoleDeveloper, "d"),
				text(protocol.RoleUser, "q"),
				protocol.Message{
					ID:      "msg_1",
					Status:  protocol.StatusCompleted,
					Role:    protocol.RoleAssistant,
					Content: []protocol.ContentPart{protocol.OutputText{Text: "Partly:"}, protocol.Refusal{Refusal: "not that"}},
				},
				text(protocol.RoleUser, "ok"),
			),
			want: `{"model":"m",` + stateless + `,"input":[
				{"type":"message","role":"system","content":[{"type":"input_text","text":"s"}]},
				{"type":"message","role":"developer","content":[{"type":"input_text","text":"d"}]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"q"}]},
				{"type":"message","id":"msg_1","role":"assistant","content":[
					{"type":"output_text","text":"Partly:"},{"type":"refusal","refusal":"not that"}]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"ok"}]}]}`,
		},
		{
			name: "input_image by data URL with its detail, and by URL with none",
			req: ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				protocol.InputText{Text: "Compare."},
				protocol.InputImage{
					ImageURL: protocol.DataURL("image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}),
					Detail:   protocol.ImageDetailLow,
				},
				protocol.InputImage{ImageURL: "https://example.com/shapes.png"},
			}}),
			want: `{"model":"m",` + stateless + `,"input":[{"type":"message","role":"user","content":[
				{"type":"input_text","text":"Compare."},
				{"type":"input_image","image_url":"data:image/png;base64,iVBORw0KGgo=","detail":"low"},
				{"type":"input_image","image_url":"https://example.com/shapes.png"}]}]}`,
		},
		{
			// The extension's literal, pinned: its type carries the implementor slug, and its
			// data is the clip in standard base64.
			name: "input_audio as the client extension",
			req: ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				protocol.InputText{Text: "Transcribe."},
				protocol.InputAudio{Data: []byte("RIFF\x00\x01"), Format: protocol.AudioFormatWAV},
				protocol.InputAudio{Data: []byte{0xff, 0xfb}, Format: protocol.AudioFormatMP3},
			}}),
			want: `{"model":"m",` + stateless + `,"input":[{"type":"message","role":"user","content":[
				{"type":"input_text","text":"Transcribe."},
				{"type":"spike-ai-pipeline:input_audio","data":"UklGRgAB","format":"wav"},
				{"type":"spike-ai-pipeline:input_audio","data":"//s=","format":"mp3"}]}]}`,
		},
		{
			name: "text's plain format and verbosity",
			req: &protocol.Request{
				Model: "m",
				Input: []protocol.Item{text(protocol.RoleUser, "hi")},
				Text:  protocol.TextParam{Format: protocol.PlainTextFormat{}, Verbosity: protocol.VerbosityLow},
			},
			want: `{"model":"m","text":{"format":{"type":"text"},"verbosity":"low"},` + stateless + `,"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`,
		},
		{
			name: "text's json_schema format, whole",
			req: &protocol.Request{
				Model: "m",
				Input: []protocol.Item{text(protocol.RoleUser, "hi")},
				Text: protocol.TextParam{Format: protocol.JSONSchemaFormat{
					Name:        "city",
					Description: "A city.",
					Schema:      jsontext.Value(`{"type":"object","properties":{"name":{"type":"string"}}}`),
					Strict:      true,
				}},
			},
			want: `{"model":"m","text":{"format":{"type":"json_schema","name":"city","description":"A city.",
				"schema":{"type":"object","properties":{"name":{"type":"string"}}},"strict":true}},` + stateless + `,"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`,
		},
		{
			name: "text's json_schema format, its description and strict flag unset",
			req: &protocol.Request{
				Model: "m",
				Input: []protocol.Item{text(protocol.RoleUser, "hi")},
				Text:  protocol.TextParam{Format: protocol.JSONSchemaFormat{Name: "city", Schema: jsontext.Value(`{"type":"object"}`)}},
			},
			want: `{"model":"m","text":{"format":{"type":"json_schema","name":"city","schema":{"type":"object"}}},` + stateless + `,"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`,
		},
		{
			name: "native members merged into the top level",
			req:  withNative(ask(text(protocol.RoleUser, "hi")), `{"temperature":0,"seed":7}`),
			want: `{"model":"m","temperature":0,"seed":7,` + stateless + `,"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertJSON(t, encode(t, tc.req), tc.want)
		})
	}
}

// TestEncodeStreamRequest covers a streamed request: EncodeRequest's body with stream true.
func TestEncodeStreamRequest(t *testing.T) {
	req := ask(text(protocol.RoleUser, "hi"))
	header := make(http.Header)
	body, err := responses.New(responses.Options{}).EncodeStreamRequest(req, header)
	if err != nil {
		t.Fatalf("EncodeStreamRequest: %v", err)
	}
	assertJSON(t, body, `{"model":"m","stream":true,"store":false,"include":["reasoning.encrypted_content"],"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
	if len(header) != 0 {
		t.Errorf("EncodeStreamRequest set headers %v, want none", header)
	}
}

// TestEncodeRefuses covers the valid requests the format refuses.
func TestEncodeRefuses(t *testing.T) {
	user := text(protocol.RoleUser, "hi")
	cases := []struct {
		name string
		req  *protocol.Request
		want string
	}{
		{"native stream", withNative(ask(user), `{"stream":true}`),
			"native member stream clashes with a member the request sets"},
		{"native store", withNative(ask(user), `{"store":true}`),
			"native member store clashes with a member the request sets"},
		{"native include", withNative(ask(user), `{"include":[]}`),
			"native member include clashes with a member the request sets"},
		{"native input", withNative(ask(user), `{"input":"hi"}`),
			"native member input clashes with a member the request sets"},
		{"native max_output_tokens beside a typed bound", withNative(&protocol.Request{Model: "m", Input: []protocol.Item{user},
			MaxOutputTokens: 64}, `{"max_output_tokens":8}`),
			"native member max_output_tokens clashes with a member the request sets"},
		{"native text beside a typed verbosity", withNative(&protocol.Request{Model: "m", Input: []protocol.Item{user},
			Text: protocol.TextParam{Verbosity: protocol.VerbosityHigh}}, `{"text":{}}`),
			"native member text clashes with a member the request sets"},
		{"native previous_response_id", withNative(ask(user), `{"previous_response_id":"resp_1"}`),
			"native member previous_response_id asks the endpoint to keep state"},
		{"native background", withNative(ask(user), `{"background":false}`),
			"native member background asks the endpoint to keep state"},
		{"native tools beside typed tools", withNative(&protocol.Request{Model: "m", Input: []protocol.Item{user},
			Tools: []protocol.FunctionTool{{Name: "f"}}}, `{"tools":[]}`),
			"native member tools clashes with a member the request sets"},
		{"native tool_choice beside a typed one", withNative(&protocol.Request{Model: "m", Input: []protocol.Item{user},
			ToolChoice: protocol.ToolChoiceAuto}, `{"tool_choice":"none"}`),
			"native member tool_choice clashes with a member the request sets"},
		{"native reasoning beside a typed effort", withNative(&protocol.Request{Model: "m", Input: []protocol.Item{user},
			Reasoning: protocol.ReasoningParam{Effort: protocol.ReasoningEffortLow}}, `{"reasoning":{}}`),
			"native member reasoning clashes with a member the request sets"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			body, err := responses.New(responses.Options{}).EncodeRequest(tc.req, make(http.Header))
			if err == nil {
				t.Fatalf("EncodeRequest = %s, nil; want an error containing %q", body, tc.want)
			}
			if !strings.HasPrefix(err.Error(), "responses: ") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("EncodeRequest error = %q, want it to start responses: and contain %q", err, tc.want)
			}
		})
	}
}

// textReasoning is the reasoning text of the reply recorded in testdata/text.json.
const textReasoning = `The user asks: "Name three French cities, comma-separated, nothing else." The developer says: "Answer with the names alone." So we should output three French city names, separated by commas, no extra text. So something like "Paris, Lyon, Marseille". Ensure no extra punctuation beyond commas. Probably just that.`

// toolCallReasoning is the reasoning text of the reply recorded in testdata/tool_call.json.
const toolCallReasoning = "User asks current weather in Paris. Use function get_weather."

// TestDecodeRecorded decodes the router's replies recorded in testdata.
func TestDecodeRecorded(t *testing.T) {
	cases := []struct {
		file string
		want *protocol.Response
	}{
		{
			// The reasoning item ahead of the message, its summary and encrypted_content empty,
			// and the item ids the router's.
			file: "text.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					protocol.Reasoning{
						ID:      "rs_Pt1rtkP5f8C8yiCOAXqiF7kVKWuw2TVi",
						Status:  protocol.StatusCompleted,
						Content: []protocol.ReasoningText{{Text: textReasoning}},
						// The router's encrypted_content, empty, and the reasoning text, sealed.
						EncryptedContent: seal(t, "", textReasoning),
					},
					protocol.Message{
						ID:      "msg_BlIKh9EJElGVax8sok27dAu1gSSdNMXW",
						Status:  protocol.StatusCompleted,
						Role:    protocol.RoleAssistant,
						Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris, Lyon, Marseille"}},
					},
				},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 91, OutputTokens: 80, TotalTokens: 171},
			},
		},
		{
			// gpt-oss-120b's reasoning ahead of its call, the reasoning's encrypted_content the
			// format's envelope of the router's, empty, and the reasoning text.
			file: "tool_call.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					protocol.Reasoning{
						ID:      "rs_H4pfaCFvrGf1ljLr7M56OB81gR3sEvYK",
						Status:  protocol.StatusCompleted,
						Content: []protocol.ReasoningText{{Text: toolCallReasoning}},
						// The envelope's literal, pinned: base64 of
						// {"encrypted_content":"","content":["User asks current weather in Paris. Use function get_weather."]}.
						EncryptedContent: "spike-ai-pipeline:responses/v1:" +
							"eyJlbmNyeXB0ZWRfY29udGVudCI6IiIsImNvbnRlbnQiOlsiVXNlciBhc2tzIGN1cnJlbnQgd2VhdGhlciBpbiBQYXJpcy4gVXNlIGZ1bmN0aW9uIGdldF93ZWF0aGVyLiJdfQ==",
					},
					protocol.FunctionCall{
						ID:        "fc_HMhSOwdRkvjir1D59ezHFChk5pTnYdJc",
						Status:    protocol.StatusCompleted,
						CallID:    "call_HMhSOwdRkvjir1D59ezHFChk5pTnYdJc",
						Name:      "get_weather",
						Arguments: "{\n  \"city\": \"Paris\"\n}",
					},
				},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 138, OutputTokens: 40, TotalTokens: 178},
			},
		},
		{
			// Stopped at max_output_tokens, and completed as the router reports it.
			file: "length.json",
			want: &protocol.Response{
				Output: []protocol.Item{protocol.Message{
					ID:      "msg_DlxsLumQFHURGkWf3dpu8b4uqhZnpjsM",
					Status:  protocol.StatusCompleted,
					Role:    protocol.RoleAssistant,
					Content: []protocol.ContentPart{protocol.OutputText{Text: "1 2 3 4 5 6 7 8 "}},
				}},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 29, OutputTokens: 16, TotalTokens: 45},
			},
		},
		{
			file: "conversation.json",
			want: &protocol.Response{
				Output: []protocol.Item{protocol.Message{
					ID:      "msg_P6Nc6UhZ2fauy4qRLHU2lb0oDCEAEKcd",
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
			resp, err := responses.New(responses.Options{}).DecodeResponse(readTestdata(t, tc.file))
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("DecodeResponse = %#v\nwant            %#v", resp, tc.want)
			}
		})
	}
}

// TestDecode decodes replies of the spec's shapes, beyond what the router sends.
func TestDecode(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  *protocol.Response
	}{
		{
			name: "every output item type in the server's order, ids and statuses as given",
			reply: `{"id":"resp_1","object":"response","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},
				"output":[
					{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"Looked it up."}],
						"content":[{"type":"reasoning_text","text":"hm"},{"type":"reasoning_text","text":"ok"}],"encrypted_content":"opaque"},
					{"type":"function_call","id":"fc_1","status":"completed","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"},
					{"type":"message","id":"msg_1","status":"incomplete","role":"assistant","content":[
						{"type":"output_text","text":"Paris is","annotations":[{"type":"url_citation","url":"https://example.com","start_index":0,"end_index":5,"title":"x"}],
							"logprobs":[{"token":"Paris","logprob":-0.1,"bytes":[80],"top_logprobs":[]}]},
						{"type":"refusal","refusal":"not that"}]}],
				"usage":{"input_tokens":20,"output_tokens":16,"total_tokens":36,
					"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":9}}}`,
			want: &protocol.Response{
				Output: []protocol.Item{
					protocol.Reasoning{
						ID:               "rs_1",
						Status:           protocol.StatusCompleted,
						Content:          []protocol.ReasoningText{{Text: "hm"}, {Text: "ok"}},
						Summary:          []protocol.SummaryText{{Text: "Looked it up."}},
						EncryptedContent: seal(t, "opaque", "hm", "ok"),
					},
					protocol.FunctionCall{ID: "fc_1", Status: protocol.StatusCompleted, CallID: "call_1", Name: "get_weather",
						Arguments: `{"city":"Paris"}`},
					protocol.Message{
						ID:      "msg_1",
						Status:  protocol.StatusIncomplete,
						Role:    protocol.RoleAssistant,
						Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris is"}, protocol.Refusal{Refusal: "not that"}},
					},
				},
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
				Usage: protocol.Usage{InputTokens: 20, OutputTokens: 16, TotalTokens: 36,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 9}},
			},
		},
		{
			name: "incomplete at the content filter",
			reply: `{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"error":null,
				"output":[{"type":"message","id":"msg_2","status":"incomplete","role":"assistant","content":[{"type":"refusal","refusal":"No."}]}]}`,
			want: &protocol.Response{
				Output: []protocol.Item{protocol.Message{ID: "msg_2", Status: protocol.StatusIncomplete, Role: protocol.RoleAssistant,
					Content: []protocol.ContentPart{protocol.Refusal{Refusal: "No."}}}},
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonContentFilter},
			},
		},
		{
			name:  "failed, with its error",
			reply: `{"status":"failed","incomplete_details":null,"error":{"code":"server_error","message":"The model crashed."},"output":[]}`,
			want: &protocol.Response{
				Status: protocol.StatusFailed,
				Error:  &protocol.ResponseError{Code: "server_error", Message: "The model crashed."},
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

func TestDecodeRefusesReply(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  string
	}{
		{"output item of unknown type", `{"status":"completed","output":[
			{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[]},
			{"type":"web_search_call","id":"ws_1","status":"completed"}]}`, `output item 1: unknown type "web_search_call"`},
		{"message part of unknown type", `{"status":"completed","output":[
			{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"input_text","text":"x"}]}]}`,
			`output item 0: message content 0 has unknown type "input_text"`},
		{"reasoning part of unknown type", `{"status":"completed","output":[
			{"type":"reasoning","id":"rs_1","summary":[],"content":[{"type":"output_text","text":"x"}]}]}`,
			`output item 0: reasoning content 0 has unknown type "output_text"`},
		{"summary part of unknown type", `{"status":"completed","output":[
			{"type":"reasoning","id":"rs_1","summary":[{"type":"reasoning_text","text":"x"}]}]}`,
			`output item 0: reasoning summary 0 has unknown type "reasoning_text"`},
		{"in_progress", `{"status":"in_progress","output":[]}`, `response has status "in_progress"`},
		{"queued", `{"status":"queued","output":[]}`, `response has status "queued"`},
		{"no status", `{"output":[]}`, `response has status ""`},
		{"not JSON", `Paris`, "decode response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := decode(tc.reply)
			if err == nil {
				t.Fatalf("DecodeResponse = %v, nil; want an error containing %q", resp, tc.want)
			}
			if !strings.HasPrefix(err.Error(), "responses: ") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("DecodeResponse error = %q, want it to start responses: and contain %q", err, tc.want)
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
		{"unknown model, recorded", string(readTestdata(t, "model_not_found.json")),
			"model 'spike-ai-pipeline/no-such-model' not found", "invalid_request_error", "400"},
		{"the audio extension, recorded", string(readTestdata(t, "input_audio.json")),
			"'type' must be one of 'input_text', 'input_image', or 'input_file'", "invalid_request_error", "400"},
		{"a string code", `{"error":{"message":"Slow down.","type":"rate_limit_error","code":"rate_limit_exceeded"}}`,
			"Slow down.", "rate_limit_error", "rate_limit_exceeded"},
		{"not JSON", "upstream down\n", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			message, typ, code := responses.New(responses.Options{}).DecodeError([]byte(tc.body))
			if message != tc.message || typ != tc.typ || code != tc.code {
				t.Errorf("DecodeError = {%q %q %q}, want {%q %q %q}",
					message, typ, code, tc.message, tc.typ, tc.code)
			}
		})
	}
}

// TestStreamDecoderRefuses covers this version's stream decoder, which refuses every stream.
func TestStreamDecoderRefuses(t *testing.T) {
	d := responses.New(responses.Options{}).NewStreamDecoder()
	if events, err := d.Decode([]byte(`{"type":"response.created"}`)); err == nil {
		t.Errorf("Decode = %v, nil; want an error", events)
	}
	if events, err := d.End(); err == nil {
		t.Errorf("End = %v, nil; want an error", events)
	}
}
