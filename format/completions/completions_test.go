package completions_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/format/completions"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// encode encodes req, failing t if it is invalid or EncodeRequest refuses it, or if
// EncodeRequest adds a header, which the format never does. The client validates every request
// before encoding it, so a request the format is given here is a valid one.
func encode(t *testing.T, req *protocol.Request) []byte {
	t.Helper()
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	header := make(http.Header)
	body, err := completions.New().EncodeRequest(req, header)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	if len(header) != 0 {
		t.Errorf("EncodeRequest added headers %v, want none", header)
	}
	return body
}

// decode decodes reply as the body of a 2xx reply.
func decode(reply string) (*protocol.Response, error) {
	return completions.New().DecodeResponse([]byte(reply))
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

// weather is a function tool with every field set.
var weather = protocol.FunctionTool{
	Name:        "get_weather",
	Description: "Get the current weather in a city.",
	Parameters:  jsontext.Value(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	Strict:      true,
}

// withTools is a request of one user message with weather and a get_time tool of name alone,
// and the tool choice tc.
func withTools(tc protocol.ToolChoice) *protocol.Request {
	return &protocol.Request{
		Model:      "m",
		Input:      []protocol.Item{text(protocol.RoleUser, "hi")},
		Tools:      []protocol.FunctionTool{weather, {Name: "get_time"}},
		ToolChoice: tc,
	}
}

// toolsWire is the wire tools of withTools.
const toolsWire = `"tools":[
	{"type":"function","function":{"name":"get_weather","description":"Get the current weather in a city.",
		"parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":true}},
	{"type":"function","function":{"name":"get_time"}}]`

func assistant(s string) protocol.Message {
	return protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: s}}}
}

// The format's envelopes, as it writes them into encrypted_content, of the reasoning_content
// their names give. They are literal so a change to the envelope's layout, which would orphan
// the envelopes callers hold, fails here.
const (
	envelopeUserAsks    = "spike-ai-pipeline:completions/v1:eyJyZWFzb25pbmdfY29udGVudCI6IlRoZSB1c2VyIGFza3MuIn0="
	envelopeWeatherTool = "spike-ai-pipeline:completions/v1:eyJyZWFzb25pbmdfY29udGVudCI6Ik5lZWQgdGhlIHdlYXRoZXIgdG9vbC4ifQ=="
	envelopeThenAnswer  = "spike-ai-pipeline:completions/v1:eyJyZWFzb25pbmdfY29udGVudCI6IlRoZW4gYW5zd2VyLiJ9"
	envelopeEmpty       = "spike-ai-pipeline:completions/v1:eyJyZWFzb25pbmdfY29udGVudCI6IiJ9"
)

// reasoning is a reasoning item of the envelope encrypted, with content its format ignores on
// input.
func reasoning(encrypted string) protocol.Reasoning {
	return protocol.Reasoning{
		Content:          []protocol.ReasoningText{{Text: "content is never sent"}},
		Summary:          []protocol.SummaryText{{Text: "nor is a summary"}},
		EncryptedContent: encrypted,
	}
}

func TestEncode(t *testing.T) {
	cases := []struct {
		name string
		req  *protocol.Request
		want string
	}{
		{
			name: "single input_text as a string",
			req:  &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")}},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "instructions as a leading system message",
			req: &protocol.Request{
				Model:        "m",
				Instructions: "Answer in one word.",
				Input:        []protocol.Item{text(protocol.RoleUser, "Capital of France?")},
			},
			want: `{"model":"m","messages":[
				{"role":"system","content":"Answer in one word."},
				{"role":"user","content":"Capital of France?"}]}`,
		},
		{
			name: "several parts as an array",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{protocol.Message{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{protocol.InputText{Text: "one"}, protocol.InputText{Text: "two"}},
			}}},
			want: `{"model":"m","messages":[{"role":"user","content":[
				{"type":"text","text":"one"},{"type":"text","text":"two"}]}]}`,
		},
		{
			name: "each message its role",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleSystem, "s"),
				text(protocol.RoleDeveloper, "d"),
				text(protocol.RoleUser, "u"),
				protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "a"}}},
				text(protocol.RoleUser, "u2"),
			}},
			want: `{"model":"m","messages":[
				{"role":"system","content":"s"},
				{"role":"developer","content":"d"},
				{"role":"user","content":"u"},
				{"role":"assistant","content":"a"},
				{"role":"user","content":"u2"}]}`,
		},
		{
			name: "assistant output_text and refusal as an array",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
					protocol.OutputText{Text: "Partly:"}, protocol.Refusal{Refusal: "not that"},
				}},
				text(protocol.RoleUser, "ok"),
			}},
			want: `{"model":"m","messages":[
				{"role":"assistant","content":[{"type":"text","text":"Partly:"},{"type":"refusal","refusal":"not that"}]},
				{"role":"user","content":"ok"}]}`,
		},
		{
			name: "single refusal as an array",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.Refusal{Refusal: "no"}}},
			}},
			want: `{"model":"m","messages":[{"role":"assistant","content":[{"type":"refusal","refusal":"no"}]}]}`,
		},
		{
			name: "input_image by data URL as image_url, detail omitted when unset",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{protocol.Message{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{
					protocol.InputText{Text: "Describe this image."},
					protocol.InputImage{ImageURL: protocol.DataURL("image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})},
				},
			}}},
			want: `{"model":"m","messages":[{"role":"user","content":[
				{"type":"text","text":"Describe this image."},
				{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]}]}`,
		},
		{
			name: "single input_image by remote URL with detail as an array",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{protocol.Message{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{
					protocol.InputImage{ImageURL: "https://example.com/shapes.png", Detail: protocol.ImageDetailHigh},
				},
			}}},
			want: `{"model":"m","messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"https://example.com/shapes.png","detail":"high"}}]}]}`,
		},
		{
			name: "input_audio with its bytes in base64",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{protocol.Message{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{
					protocol.InputText{Text: "Transcribe this clip."},
					protocol.InputAudio{Data: []byte("RIFF\x24\x00\x00\x00WAVE"), Format: protocol.AudioFormatWAV},
				},
			}}},
			want: `{"model":"m","messages":[{"role":"user","content":[
				{"type":"text","text":"Transcribe this clip."},
				{"type":"input_audio","input_audio":{"data":"UklGRiQAAABXQVZF","format":"wav"}}]}]}`,
		},
		{
			name: "single input_audio as an array",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{protocol.Message{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{protocol.InputAudio{Data: []byte{0xff, 0xfb, 0x90}, Format: protocol.AudioFormatMP3}},
			}}},
			want: `{"model":"m","messages":[{"role":"user","content":[
				{"type":"input_audio","input_audio":{"data":"//uQ","format":"mp3"}}]}]}`,
		},
		{
			name: "tools as function tools, unset members omitted",
			req:  withTools(nil),
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],` + toolsWire + `}`,
		},
		{
			name: "tool_choice auto",
			req:  withTools(protocol.ToolChoiceAuto),
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],` + toolsWire + `,"tool_choice":"auto"}`,
		},
		{
			name: "tool_choice none",
			req:  withTools(protocol.ToolChoiceNone),
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],` + toolsWire + `,"tool_choice":"none"}`,
		},
		{
			name: "tool_choice required",
			req:  withTools(protocol.ToolChoiceRequired),
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],` + toolsWire + `,"tool_choice":"required"}`,
		},
		{
			name: "tool_choice of a named function",
			req:  withTools(protocol.FunctionToolChoice{Name: "get_weather"}),
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],` + toolsWire + `,
				"tool_choice":{"type":"function","function":{"name":"get_weather"}}}`,
		},
		{
			name: "tool_choice of allowed tools, auto",
			req: withTools(protocol.AllowedToolChoice{
				Tools: []protocol.FunctionToolChoice{{Name: "get_time"}},
				Mode:  protocol.ToolChoiceAuto,
			}),
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],` + toolsWire + `,
				"tool_choice":{"type":"allowed_tools","allowed_tools":{"mode":"auto",
					"tools":[{"type":"function","function":{"name":"get_time"}}]}}}`,
		},
		{
			name: "tool_choice of allowed tools, required",
			req: withTools(protocol.AllowedToolChoice{
				Tools: []protocol.FunctionToolChoice{{Name: "get_weather"}, {Name: "get_time"}},
				Mode:  protocol.ToolChoiceRequired,
			}),
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],` + toolsWire + `,
				"tool_choice":{"type":"allowed_tools","allowed_tools":{"mode":"required","tools":[
					{"type":"function","function":{"name":"get_weather"}},
					{"type":"function","function":{"name":"get_time"}}]}}}`,
		},
		{
			name: "assistant message and its calls as one message",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleUser, "Weather and time in Paris?"),
				assistant("Checking."),
				protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
				protocol.FunctionCall{ID: "fc_2", Status: protocol.StatusCompleted, CallID: "c2", Name: "get_time"},
				protocol.FunctionCallOutput{CallID: "c1", Output: `{"celsius":21}`},
				protocol.FunctionCallOutput{CallID: "c2", Output: "09:00"},
			}},
			want: `{"model":"m","messages":[
				{"role":"user","content":"Weather and time in Paris?"},
				{"role":"assistant","content":"Checking.","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}},
					{"id":"c2","type":"function","function":{"name":"get_time","arguments":"{}"}}]},
				{"role":"tool","content":"{\"celsius\":21}","tool_call_id":"c1"},
				{"role":"tool","content":"09:00","tool_call_id":"c2"}]}`,
		},
		{
			name: "calls alone as an assistant message with no content",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleUser, "Weather in Paris?"),
				protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
				protocol.FunctionCallOutput{CallID: "c1", Output: `{"celsius":21}`},
			}},
			want: `{"model":"m","messages":[
				{"role":"user","content":"Weather in Paris?"},
				{"role":"assistant","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
				{"role":"tool","content":"{\"celsius\":21}","tool_call_id":"c1"}]}`,
		},
		{
			name: "two runs separated by tool outputs",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleUser, "Weather in Paris, then Rome?"),
				protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
				protocol.FunctionCallOutput{CallID: "c1", Output: "21"},
				assistant("Paris is 21. Now Rome."),
				protocol.FunctionCall{CallID: "c2", Name: "get_weather", Arguments: `{"city":"Rome"}`},
				protocol.FunctionCallOutput{CallID: "c2", Output: "25"},
				protocol.FunctionCall{CallID: "c3", Name: "get_weather", Arguments: `{"city":"Oslo"}`},
				protocol.FunctionCallOutput{CallID: "c3", Output: "9"},
				assistant("Paris 21, Rome 25, Oslo 9."),
			}},
			want: `{"model":"m","messages":[
				{"role":"user","content":"Weather in Paris, then Rome?"},
				{"role":"assistant","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
				{"role":"tool","content":"21","tool_call_id":"c1"},
				{"role":"assistant","content":"Paris is 21. Now Rome.","tool_calls":[
					{"id":"c2","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Rome\"}"}}]},
				{"role":"tool","content":"25","tool_call_id":"c2"},
				{"role":"assistant","tool_calls":[
					{"id":"c3","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Oslo\"}"}}]},
				{"role":"tool","content":"9","tool_call_id":"c3"},
				{"role":"assistant","content":"Paris 21, Rome 25, Oslo 9."}]}`,
		},
		{
			name: "a user message closes a run",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				assistant("Which city?"),
				text(protocol.RoleUser, "Paris."),
				protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
			}},
			want: `{"model":"m","messages":[
				{"role":"assistant","content":"Which city?"},
				{"role":"user","content":"Paris."},
				{"role":"assistant","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}]}`,
		},
		{
			name: "tool outputs: empty, and parts",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				protocol.FunctionCall{CallID: "c1", Name: "ping"},
				protocol.FunctionCall{CallID: "c2", Name: "get_weather", Arguments: `{"city":"Paris"}`},
				protocol.FunctionCall{CallID: "c3", Name: "get_weather", Arguments: `{"city":"Rome"}`},
				protocol.FunctionCallOutput{CallID: "c1"},
				protocol.FunctionCallOutput{CallID: "c2", OutputParts: []protocol.ContentPart{
					protocol.InputText{Text: "21"}, protocol.InputText{Text: "celsius"},
				}},
				protocol.FunctionCallOutput{CallID: "c3", OutputParts: []protocol.ContentPart{protocol.InputText{Text: "25"}}},
			}},
			want: `{"model":"m","messages":[
				{"role":"assistant","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"ping","arguments":"{}"}},
					{"id":"c2","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}},
					{"id":"c3","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Rome\"}"}}]},
				{"role":"tool","content":"","tool_call_id":"c1"},
				{"role":"tool","content":[{"type":"text","text":"21"},{"type":"text","text":"celsius"}],"tool_call_id":"c2"},
				{"role":"tool","content":"25","tool_call_id":"c3"}]}`,
		},
		{
			name: "max_output_tokens as max_completion_tokens",
			req:  &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")}, MaxOutputTokens: 64},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":64}`,
		},
		{
			name: "reasoning effort as reasoning_effort",
			req: &protocol.Request{
				Model:     "m",
				Input:     []protocol.Item{text(protocol.RoleUser, "hi")},
				Reasoning: protocol.ReasoningParam{Effort: protocol.ReasoningEffortXHigh},
			},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`,
		},
		{
			name: "plain text format as response_format text",
			req: &protocol.Request{
				Model: "m",
				Input: []protocol.Item{text(protocol.RoleUser, "hi")},
				Text:  protocol.TextParam{Format: protocol.PlainTextFormat{}},
			},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"text"}}`,
		},
		{
			name: "json_schema format, strict, every field set",
			req: &protocol.Request{
				Model: "m",
				Input: []protocol.Item{text(protocol.RoleUser, "Capital of France?")},
				Text: protocol.TextParam{Format: protocol.JSONSchemaFormat{
					Name:        "capital",
					Description: "A country's capital.",
					Schema:      []byte(`{"type":"object","properties":{"capital":{"type":"string"}},"required":["capital"]}`),
					Strict:      true,
				}},
			},
			want: `{"model":"m","messages":[{"role":"user","content":"Capital of France?"}],
				"response_format":{"type":"json_schema","json_schema":{
					"name":"capital",
					"description":"A country's capital.",
					"schema":{"type":"object","properties":{"capital":{"type":"string"}},"required":["capital"]},
					"strict":true}}}`,
		},
		{
			name: "json_schema format, not strict, unset members omitted",
			req: &protocol.Request{
				Model: "m",
				Input: []protocol.Item{text(protocol.RoleUser, "Capital of France?")},
				Text: protocol.TextParam{Format: protocol.JSONSchemaFormat{
					Name:   "capital",
					Schema: []byte(`{"type":"object"}`),
				}},
			},
			want: `{"model":"m","messages":[{"role":"user","content":"Capital of France?"}],
				"response_format":{"type":"json_schema","json_schema":{"name":"capital","schema":{"type":"object"}}}}`,
		},
		{
			// The spec's request has no json_object format; Chat Completions' JSON mode is
			// reached through the native tier.
			name: "native response_format json_object when text format is unset",
			req: &protocol.Request{
				Model:  "m",
				Input:  []protocol.Item{text(protocol.RoleUser, "hi")},
				Native: jsontext.Value(`{"response_format":{"type":"json_object"}}`),
			},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_object"}}`,
		},
		{
			name: "verbosity as verbosity",
			req: &protocol.Request{
				Model: "m",
				Input: []protocol.Item{text(protocol.RoleUser, "hi")},
				Text:  protocol.TextParam{Verbosity: protocol.VerbosityLow},
			},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],"verbosity":"low"}`,
		},
		{
			name: "reasoning joins the assistant message and calls after it",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleUser, "Weather in Paris?"),
				reasoning(envelopeWeatherTool),
				assistant("Checking."),
				protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
				protocol.FunctionCallOutput{CallID: "c1", Output: "21"},
				reasoning(envelopeThenAnswer),
				assistant("It is 21."),
			}},
			want: `{"model":"m","messages":[
				{"role":"user","content":"Weather in Paris?"},
				{"role":"assistant","content":"Checking.","reasoning_content":"Need the weather tool.","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
				{"role":"tool","content":"21","tool_call_id":"c1"},
				{"role":"assistant","content":"It is 21.","reasoning_content":"Then answer."}]}`,
		},
		{
			name: "reasoning opens a run of calls alone",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleUser, "Weather in Paris?"),
				reasoning(envelopeWeatherTool),
				protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
			}},
			want: `{"model":"m","messages":[
				{"role":"user","content":"Weather in Paris?"},
				{"role":"assistant","reasoning_content":"Need the weather tool.","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}]}`,
		},
		{
			name: "consecutive reasoning concatenated in order",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleUser, "hi"),
				reasoning(envelopeUserAsks),
				reasoning(envelopeThenAnswer),
				assistant("Hello."),
			}},
			want: `{"model":"m","messages":[
				{"role":"user","content":"hi"},
				{"role":"assistant","content":"Hello.","reasoning_content":"The user asks.Then answer."}]}`,
		},
		{
			name: "reasoning opens a new run",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleUser, "hi"),
				assistant("Hello."),
				reasoning(envelopeWeatherTool),
				protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{}`},
			}},
			want: `{"model":"m","messages":[
				{"role":"user","content":"hi"},
				{"role":"assistant","content":"Hello."},
				{"role":"assistant","reasoning_content":"Need the weather tool.","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}]}`,
		},
		{
			name: "reasoning not this format's dropped",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleUser, "hi"),
				reasoning(""),
				reasoning("gAAAAABo-opaque-provider-blob"),
				reasoning("spike-ai-pipeline:messages/v1:eyJ0aGlua2luZyI6IngifQ=="),
				reasoning("spike-ai-pipeline:completions/v2:" + strings.TrimPrefix(envelopeUserAsks, "spike-ai-pipeline:completions/v1:")),
				reasoning("spike-ai-pipeline:completions/v1:not base64!"),
				reasoning("spike-ai-pipeline:completions/v1:bm90IEpTT04="),
				reasoning(envelopeEmpty),
				assistant("Hello."),
			}},
			want: `{"model":"m","messages":[
				{"role":"user","content":"hi"},
				{"role":"assistant","content":"Hello."}]}`,
		},
		{
			name: "dropped reasoning leaves the run open",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleUser, "hi"),
				assistant("Checking."),
				reasoning("gAAAAABo-opaque-provider-blob"),
				protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{}`},
			}},
			want: `{"model":"m","messages":[
				{"role":"user","content":"hi"},
				{"role":"assistant","content":"Checking.","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}]}`,
		},
		{
			name: "reasoning nothing follows dropped",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{
				text(protocol.RoleUser, "hi"),
				reasoning(envelopeUserAsks),
				text(protocol.RoleUser, "Still there?"),
				reasoning(envelopeThenAnswer),
			}},
			want: `{"model":"m","messages":[
				{"role":"user","content":"hi"},
				{"role":"user","content":"Still there?"}]}`,
		},
		{
			name: "native members at the top level",
			req: &protocol.Request{
				Model:  "m",
				Input:  []protocol.Item{text(protocol.RoleUser, "hi")},
				Native: jsontext.Value(`{"temperature":0.2,"chat_template_kwargs":{"reasoning_effort":"low"}}`),
			},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],
				"temperature":0.2,"chat_template_kwargs":{"reasoning_effort":"low"}}`,
		},
		{
			name: "native member the typed fields leave unset",
			req: &protocol.Request{
				Model:  "m",
				Input:  []protocol.Item{text(protocol.RoleUser, "hi")},
				Native: jsontext.Value(`{"max_completion_tokens":32}`),
			},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":32}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertJSON(t, encode(t, tc.req), tc.want)
		})
	}
}

func TestRoute(t *testing.T) {
	if got := completions.New().Route(); got != "chat/completions" {
		t.Errorf("Route = %q, want chat/completions", got)
	}
}

// TestReasoningRoundTrip sends a reply's output back as input, and wants the reasoning_content
// it came with sent back exactly, on the assistant message it led to.
func TestReasoningRoundTrip(t *testing.T) {
	const reasoningContent = "The user wants Paris's weather.\n\nCall get_weather with {\"city\":\"Paris\"} — \u00e9t\u00e9 \U0001F600."
	reply, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"finish_reason": "tool_calls",
			"message": map[string]any{
				"role":              "assistant",
				"reasoning_content": reasoningContent,
				"tool_calls": []any{map[string]any{"id": "c1", "type": "function",
					"function": map[string]any{"name": "get_weather", "arguments": `{"city":"Paris"}`}}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "Weather in Paris?")}}
	resp, err := decode(string(reply))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	r, ok := resp.Output[0].(protocol.Reasoning)
	if !ok || len(r.Content) != 1 || r.Content[0].Text != reasoningContent {
		t.Fatalf("Output[0] = %#v, want reasoning of the reply's reasoning_content", resp.Output[0])
	}
	req.Input = append(req.Input, resp.Output...)
	req.Input = append(req.Input, protocol.FunctionCallOutput{CallID: "c1", Output: "21"})
	var body struct {
		Messages []struct {
			Role             string `json:"role"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(encode(t, req), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 3 || body.Messages[1].Role != "assistant" {
		t.Fatalf("messages = %+v, want user, assistant, and tool", body.Messages)
	}
	if got := body.Messages[1].ReasoningContent; got != reasoningContent {
		t.Errorf("reasoning_content = %q, want %q", got, reasoningContent)
	}
}

func TestEncodeRefusesNativeClash(t *testing.T) {
	jsonSchema := protocol.TextParam{Format: protocol.JSONSchemaFormat{Name: "capital", Schema: []byte(`{}`)}}
	cases := []struct {
		name   string
		max    int
		text   protocol.TextParam
		native string
		member string
	}{
		{"model", 0, protocol.TextParam{}, `{"model":"other"}`, "model"},
		{"messages", 0, protocol.TextParam{}, `{"temperature":0.2,"messages":[]}`, "messages"},
		{"max_completion_tokens when set", 64, protocol.TextParam{}, `{"max_completion_tokens":32}`, "max_completion_tokens"},
		{"response_format when set", 0, jsonSchema, `{"response_format":{"type":"json_object"}}`, "response_format"},
		{"verbosity when set", 0, protocol.TextParam{Verbosity: protocol.VerbosityHigh}, `{"verbosity":"low"}`, "verbosity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := completions.New().EncodeRequest(&protocol.Request{
				Model:           "m",
				Input:           []protocol.Item{text(protocol.RoleUser, "hi")},
				MaxOutputTokens: tc.max,
				Text:            tc.text,
				Native:          jsontext.Value(tc.native),
			}, make(http.Header))
			if err == nil {
				t.Fatalf("EncodeRequest = %s, nil; want a clash error", body)
			}
			want := "native member " + tc.member + " clashes"
			if !strings.Contains(err.Error(), want) {
				t.Errorf("EncodeRequest error = %q, want it to contain %q", err, want)
			}
		})
	}
}

func TestEncodeRefusesAllowedToolsModeNone(t *testing.T) {
	body, err := completions.New().EncodeRequest(withTools(protocol.AllowedToolChoice{
		Tools: []protocol.FunctionToolChoice{{Name: "get_time"}},
		Mode:  protocol.ToolChoiceNone,
	}), make(http.Header))
	if err == nil {
		t.Fatalf("EncodeRequest = %s, nil; want an error", body)
	}
	if want := "allowed_tools of mode none has no Chat Completions form"; !strings.Contains(err.Error(), want) {
		t.Errorf("EncodeRequest error = %q, want it to contain %q", err, want)
	}
}

func TestDecodeFinishReason(t *testing.T) {
	cases := []struct {
		reason  string
		status  protocol.Status
		details *protocol.IncompleteDetails
	}{
		{"stop", protocol.StatusCompleted, nil},
		{"tool_calls", protocol.StatusCompleted, nil},
		{"length", protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}},
		{"content_filter", protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonContentFilter}},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			reply := `{"choices":[{"message":{"role":"assistant","content":"Paris"},"finish_reason":"` + tc.reason + `"}]}`
			resp, err := decode(reply)
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

func TestDecode(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  *protocol.Response
	}{
		{
			name: "llama.cpp reply with reasoning_content and usage",
			reply: `{"choices":[{"finish_reason":"stop","index":0,"message":{"role":"assistant",
				"content":"Paris","reasoning_content":"The user asks."}}],
				"created":1793041885,"model":"m","object":"chat.completion",
				"usage":{"completion_tokens":20,"prompt_tokens":83,"total_tokens":103,
					"prompt_tokens_details":{"cached_tokens":56}},
				"id":"chatcmpl-1","timings":{"prompt_n":27}}`,
			want: &protocol.Response{
				Output: []protocol.Item{
					protocol.Reasoning{
						Content:          []protocol.ReasoningText{{Text: "The user asks."}},
						EncryptedContent: envelopeUserAsks,
					},
					protocol.Message{
						Status:  protocol.StatusCompleted,
						Role:    protocol.RoleAssistant,
						Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}},
					},
				},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 83, OutputTokens: 20, TotalTokens: 103},
			},
		},
		{
			name: "reasoning first, then the message and the calls",
			reply: `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant",
				"content":"Checking.","reasoning_content":"Need the weather tool.",
				"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}}]}`,
			want: &protocol.Response{
				Output: []protocol.Item{
					protocol.Reasoning{
						Content:          []protocol.ReasoningText{{Text: "Need the weather tool."}},
						EncryptedContent: envelopeWeatherTool,
					},
					protocol.Message{
						Status:  protocol.StatusCompleted,
						Role:    protocol.RoleAssistant,
						Content: []protocol.ContentPart{protocol.OutputText{Text: "Checking."}},
					},
					protocol.FunctionCall{Status: protocol.StatusCompleted, CallID: "c1", Name: "get_weather", Arguments: "{}"},
				},
				Status: protocol.StatusCompleted,
			},
		},
		{
			name: "reasoning alone, cut off at the bound",
			reply: `{"choices":[{"finish_reason":"length","message":{"role":"assistant",
				"content":"","reasoning_content":"The user asks."}}]}`,
			want: &protocol.Response{
				Output: []protocol.Item{protocol.Reasoning{
					Content:          []protocol.ReasoningText{{Text: "The user asks."}},
					EncryptedContent: envelopeUserAsks,
				}},
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
			},
		},
		{
			name: "reasoning tokens",
			reply: `{"choices":[{"message":{"content":"Paris"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":5,"completion_tokens":30,"total_tokens":35,
					"completion_tokens_details":{"reasoning_tokens":28}}}`,
			want: &protocol.Response{
				Output: []protocol.Item{protocol.Message{
					Status:  protocol.StatusCompleted,
					Role:    protocol.RoleAssistant,
					Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}},
				}},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{
					InputTokens: 5, OutputTokens: 30, TotalTokens: 35,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 28},
				},
			},
		},
		{
			name:  "refusal",
			reply: `{"choices":[{"message":{"content":null,"refusal":"I can't help with that."},"finish_reason":"stop"}]}`,
			want: &protocol.Response{
				Output: []protocol.Item{protocol.Message{
					Status:  protocol.StatusCompleted,
					Role:    protocol.RoleAssistant,
					Content: []protocol.ContentPart{protocol.Refusal{Refusal: "I can't help with that."}},
				}},
				Status: protocol.StatusCompleted,
			},
		},
		{
			name:  "no content gives no message",
			reply: `{"choices":[{"message":{"content":""},"finish_reason":"length"}]}`,
			want: &protocol.Response{
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
			},
		},
		{
			name: "llama.cpp reply of tool calls, content null",
			reply: `{"choices":[{"finish_reason":"tool_calls","index":0,"message":{"role":"assistant",
				"content":null,"reasoning_content":"Need the weather tool.","tool_calls":[
					{"type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"},"id":"c1"},
					{"type":"function","function":{"name":"get_time","arguments":"{}"},"id":"c2"}]}}]}`,
			want: &protocol.Response{
				Output: []protocol.Item{
					protocol.Reasoning{
						Content:          []protocol.ReasoningText{{Text: "Need the weather tool."}},
						EncryptedContent: envelopeWeatherTool,
					},
					protocol.FunctionCall{Status: protocol.StatusCompleted, CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
					protocol.FunctionCall{Status: protocol.StatusCompleted, CallID: "c2", Name: "get_time", Arguments: "{}"},
				},
				Status: protocol.StatusCompleted,
			},
		},
		{
			name: "message before its tool calls",
			reply: `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"Checking.",
				"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}}]}`,
			want: &protocol.Response{
				Output: []protocol.Item{
					protocol.Message{
						Status:  protocol.StatusCompleted,
						Role:    protocol.RoleAssistant,
						Content: []protocol.ContentPart{protocol.OutputText{Text: "Checking."}},
					},
					protocol.FunctionCall{Status: protocol.StatusCompleted, CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
				},
				Status: protocol.StatusCompleted,
			},
		},
		{
			name: "tool call cut off at the bound",
			reply: `{"choices":[{"finish_reason":"length","message":{"role":"assistant",
				"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"ci"}}]}}]}`,
			want: &protocol.Response{
				Output: []protocol.Item{
					protocol.FunctionCall{Status: protocol.StatusIncomplete, CallID: "c1", Name: "get_weather", Arguments: `{"ci`},
				},
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
			},
		},
		{
			name: "first choice only",
			reply: `{"choices":[{"message":{"content":"one"},"finish_reason":"stop"},
				{"message":{"content":"two"},"finish_reason":"length"}]}`,
			want: &protocol.Response{
				Output: []protocol.Item{protocol.Message{
					Status:  protocol.StatusCompleted,
					Role:    protocol.RoleAssistant,
					Content: []protocol.ContentPart{protocol.OutputText{Text: "one"}},
				}},
				Status: protocol.StatusCompleted,
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
		{"unknown finish_reason", `{"choices":[{"message":{"content":"x"},"finish_reason":"function_call"}]}`, `unknown finish_reason "function_call"`},
		{"missing finish_reason", `{"choices":[{"message":{"content":"x"}}]}`, `unknown finish_reason ""`},
		{"no choices", `{"choices":[]}`, "no choices"},
		{"tool call of unknown type", `{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[
			{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}},
			{"id":"c2","type":"custom","custom":{"name":"g","input":"x"}}]}}]}`, `tool call 1 has unknown type "custom"`},
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
