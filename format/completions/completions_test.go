package completions_test

import (
	"encoding/json/jsontext"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/format/completions"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/clienttest"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// okReply is a reply that decodes to a completed response.
const okReply = `{"choices":[{"message":{"role":"assistant","content":"Paris"},"finish_reason":"stop"}]}`

// generate sends req through a Chat Completions client to a server answering reply, and returns
// the server, which holds what arrived on the wire, and Generate's results.
func generate(t *testing.T, reply string, req *protocol.Request) (*clienttest.Server, *protocol.Response, error) {
	t.Helper()
	srv := clienttest.NewServer(t, http.StatusOK, reply)
	c, err := client.New(client.Config{
		BaseURL: srv.URL + "/v1",
		HTTP:    http.DefaultClient,
		Format:  completions.New(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := c.Generate(t.Context(), req)
	return srv, resp, err
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
			srv, _, err := generate(t, okReply, tc.req)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			got := srv.Only(t)
			if got.Path != "/v1/chat/completions" {
				t.Errorf("path = %q, want /v1/chat/completions", got.Path)
			}
			assertJSON(t, got.Body, tc.want)
		})
	}
}

func TestEncodeRefusesNativeClash(t *testing.T) {
	cases := []struct {
		name   string
		max    int
		native string
		member string
	}{
		{"model", 0, `{"model":"other"}`, "model"},
		{"messages", 0, `{"temperature":0.2,"messages":[]}`, "messages"},
		{"max_completion_tokens when set", 64, `{"max_completion_tokens":32}`, "max_completion_tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, resp, err := generate(t, okReply, &protocol.Request{
				Model:           "m",
				Input:           []protocol.Item{text(protocol.RoleUser, "hi")},
				MaxOutputTokens: tc.max,
				Native:          jsontext.Value(tc.native),
			})
			if err == nil {
				t.Fatalf("Generate = %v, nil; want a clash error", resp)
			}
			want := "native member " + tc.member + " clashes"
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Generate error = %q, want it to contain %q", err, want)
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}

func TestEncodeRefusesAllowedToolsModeNone(t *testing.T) {
	srv, resp, err := generate(t, okReply, withTools(protocol.AllowedToolChoice{
		Tools: []protocol.FunctionToolChoice{{Name: "get_time"}},
		Mode:  protocol.ToolChoiceNone,
	}))
	if err == nil {
		t.Fatalf("Generate = %v, nil; want an error", resp)
	}
	if want := "allowed_tools of mode none has no Chat Completions form"; !strings.Contains(err.Error(), want) {
		t.Errorf("Generate error = %q, want it to contain %q", err, want)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
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
			_, resp, err := generate(t, reply, &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")}})
			if err != nil {
				t.Fatalf("Generate: %v", err)
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
	req := &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")}}
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
				Output: []protocol.Item{protocol.Message{
					Status:  protocol.StatusCompleted,
					Role:    protocol.RoleAssistant,
					Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}},
				}},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 83, OutputTokens: 20, TotalTokens: 103},
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
			reply: `{"choices":[{"message":{"content":"","reasoning_content":"The user"},"finish_reason":"length"}]}`,
			want: &protocol.Response{
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
			},
		},
		{
			name: "llama.cpp reply of tool calls, content null",
			reply: `{"choices":[{"finish_reason":"tool_calls","index":0,"message":{"role":"assistant",
				"content":null,"reasoning_content":"Need the weather.","tool_calls":[
					{"type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"},"id":"c1"},
					{"type":"function","function":{"name":"get_time","arguments":"{}"},"id":"c2"}]}}]}`,
			want: &protocol.Response{
				Output: []protocol.Item{
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
			_, resp, err := generate(t, tc.reply, req)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("Generate = %#v\nwant      %#v", resp, tc.want)
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
			_, resp, err := generate(t, tc.reply, &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")}})
			if err == nil {
				t.Fatalf("Generate = %v, nil; want an error containing %q", resp, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Generate error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestDecodeError covers the details an OpenAI-style error body gives the *APIError a non-2xx
// reply comes back as.
func TestDecodeError(t *testing.T) {
	cases := []struct {
		name               string
		reply              string
		message, typ, code string
	}{
		{"numeric code", `{"error":{"code":400,"message":"model 'x' not found","type":"invalid_request_error"}}`,
			"model 'x' not found", "invalid_request_error", "400"},
		{"string code", `{"error":{"message":"slow down","type":"rate_limit","code":"rate_limit_exceeded","param":null}}`,
			"slow down", "rate_limit", "rate_limit_exceeded"},
		{"message only", `{"error":{"message":"boom"}}`, "boom", "", ""},
		{"string error", `{"error":"loading model"}`, "loading model", "", ""},
		{"members before a mistyped one", `{"error":{"message":"boom","type":7,"code":"c"}}`, "boom", "", ""},
		{"error of another shape", `{"error":42}`, "", "", ""},
		{"no error member", `{"detail":"boom"}`, "", "", ""},
		{"not JSON", "upstream down\n", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, http.StatusBadRequest, tc.reply)
			c, err := client.New(client.Config{BaseURL: srv.URL, HTTP: http.DefaultClient, Format: completions.New()})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = c.Generate(t.Context(), &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")}})
			apiErr, ok := errors.AsType[*client.APIError](err)
			if !ok {
				t.Fatalf("Generate error = %v, want an *APIError", err)
			}
			if apiErr.Message != tc.message || apiErr.Type != tc.typ || apiErr.Code != tc.code {
				t.Errorf("APIError = {%q %q %q}, want {%q %q %q}",
					apiErr.Message, apiErr.Type, apiErr.Code, tc.message, tc.typ, tc.code)
			}
		})
	}
}
