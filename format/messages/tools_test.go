package messages_test

import (
	"encoding/json/jsontext"
	"reflect"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// The tests here cover what the format encodes beyond text and images: function calls and their
// outputs, reasoning sent back, tools and the tool choice, reasoning effort, the text format, and
// the native tier.

// thinkingEnvelope is the envelope of a thinking block of text "hm" and signature "sig", as
// DecodeResponse writes it: base64 of {"thinking":"hm","signature":"sig"}.
const thinkingEnvelope = "spike-ai-pipeline:messages/v1:eyJ0aGlua2luZyI6ImhtIiwic2lnbmF0dXJlIjoic2lnIn0="

// redactedEnvelope is the envelope of a redacted_thinking block of data "x": base64 of
// {"data":"x"}.
const redactedEnvelope = "spike-ai-pipeline:messages/v1:eyJkYXRhIjoieCJ9"

func call(id, name, args string) protocol.FunctionCall {
	return protocol.FunctionCall{CallID: id, Name: name, Arguments: args}
}

func output(id, s string) protocol.FunctionCallOutput {
	return protocol.FunctionCallOutput{CallID: id, Output: s}
}

func reasoning(encryptedContent string) protocol.Reasoning {
	// The content is output only, and must never reach the wire.
	return protocol.Reasoning{Content: []protocol.ReasoningText{{Text: "never sent"}}, EncryptedContent: encryptedContent}
}

func TestEncodeConversation(t *testing.T) {
	q := text(protocol.RoleUser, "q")
	cases := []struct {
		name  string
		input []protocol.Item
		want  string
	}{
		{
			name:  "a call and its output: tool_use in the assistant turn, tool_result in the user turn",
			input: []protocol.Item{q, call("c1", "get_weather", `{"city":"Paris"}`), output("c1", `{"celsius":17}`)},
			want: `[
				{"role":"user","content":[{"type":"text","text":"q"}]},
				{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"get_weather","input":{"city":"Paris"}}]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"{\"celsius\":17}"}]}]`,
		},
		{
			name:  "empty arguments as {}",
			input: []protocol.Item{q, call("c1", "now", ""), output("c1", "")},
			want: `[
				{"role":"user","content":[{"type":"text","text":"q"}]},
				{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"now","input":{}}]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":""}]}]`,
		},
		{
			name: "an assistant run: thinking, then text, then tool_use, in one message",
			input: []protocol.Item{
				q,
				reasoning(thinkingEnvelope),
				reasoning(redactedEnvelope),
				assistant(protocol.OutputText{Text: "Checking."}, protocol.Refusal{Refusal: "not that"}),
				call("c1", "f", `{"a":1}`),
				call("c2", "g", `{}`),
			},
			want: `[
				{"role":"user","content":[{"type":"text","text":"q"}]},
				{"role":"assistant","content":[
					{"type":"thinking","thinking":"hm","signature":"sig"},
					{"type":"redacted_thinking","data":"x"},
					{"type":"text","text":"Checking."},
					{"type":"text","text":"not that"},
					{"type":"tool_use","id":"c1","name":"f","input":{"a":1}},
					{"type":"tool_use","id":"c2","name":"g","input":{}}]}]`,
		},
		{
			name: "adjacent results and user content as one user turn, results first",
			input: []protocol.Item{
				q,
				call("c1", "f", `{}`),
				call("c2", "g", `{}`),
				output("c1", "one"),
				text(protocol.RoleUser, "and then"),
				protocol.FunctionCallOutput{CallID: "c2", OutputParts: []protocol.ContentPart{
					protocol.InputText{Text: "two"}, protocol.InputText{Text: "three"},
				}},
				text(protocol.RoleUser, "go on"),
			},
			want: `[
				{"role":"user","content":[{"type":"text","text":"q"}]},
				{"role":"assistant","content":[
					{"type":"tool_use","id":"c1","name":"f","input":{}},
					{"type":"tool_use","id":"c2","name":"g","input":{}}]},
				{"role":"user","content":[
					{"type":"tool_result","tool_use_id":"c1","content":"one"},
					{"type":"tool_result","tool_use_id":"c2","content":[{"type":"text","text":"two"},{"type":"text","text":"three"}]},
					{"type":"text","text":"and then"},
					{"type":"text","text":"go on"}]}]`,
		},
		{
			name: "a system message ends the user turn",
			input: []protocol.Item{
				q, call("c1", "f", `{}`), output("c1", "one"), text(protocol.RoleSystem, "s"), text(protocol.RoleUser, "u"),
			},
			want: `[
				{"role":"user","content":[{"type":"text","text":"q"}]},
				{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"f","input":{}}]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"one"}]},
				{"role":"system","content":[{"type":"text","text":"s"}]},
				{"role":"user","content":[{"type":"text","text":"u"}]}]`,
		},
		{
			name: "reasoning opens a new run, a message after calls another",
			input: []protocol.Item{
				q,
				assistant(protocol.OutputText{Text: "a"}),
				reasoning(thinkingEnvelope),
				call("c1", "f", `{}`),
				assistant(protocol.OutputText{Text: "b"}),
			},
			want: `[
				{"role":"user","content":[{"type":"text","text":"q"}]},
				{"role":"assistant","content":[{"type":"text","text":"a"}]},
				{"role":"assistant","content":[
					{"type":"thinking","thinking":"hm","signature":"sig"},
					{"type":"tool_use","id":"c1","name":"f","input":{}}]},
				{"role":"assistant","content":[{"type":"text","text":"b"}]}]`,
		},
		{
			name: "reasoning without this format's envelope dropped, never an error",
			input: []protocol.Item{
				q,
				reasoning(""),
				// Chat Completions' envelope of {"reasoning_content":"hm"}.
				reasoning("spike-ai-pipeline:completions/v1:eyJyZWFzb25pbmdfY29udGVudCI6ImhtIn0="),
				reasoning("spike-ai-pipeline:messages/v2:eyJkYXRhIjoieCJ9"),
				reasoning("spike-ai-pipeline:messages/v1:not base64!"),
				reasoning(seal(`not JSON`)),
				reasoning(seal(`{}`)),
				reasoning(seal(`{"thinking":"hm"}`)),
				reasoning(seal(`{"thinking":"hm","signature":"sig","data":"x"}`)),
				reasoning(seal(`{"thinking":1,"signature":"sig"}`)),
				assistant(protocol.OutputText{Text: "a"}),
			},
			want: `[
				{"role":"user","content":[{"type":"text","text":"q"}]},
				{"role":"assistant","content":[{"type":"text","text":"a"}]}]`,
		},
		{
			name:  "a run of reasoning alone dropped",
			input: []protocol.Item{q, assistant(protocol.OutputText{Text: "a"}), reasoning(thinkingEnvelope), text(protocol.RoleUser, "u")},
			want: `[
				{"role":"user","content":[{"type":"text","text":"q"}]},
				{"role":"assistant","content":[{"type":"text","text":"a"}]},
				{"role":"user","content":[{"type":"text","text":"u"}]}]`,
		},
		{
			name:  "an empty signature sent as it came",
			input: []protocol.Item{q, reasoning(seal(`{"thinking":"hm","signature":""}`)), call("c1", "f", `{}`)},
			want: `[
				{"role":"user","content":[{"type":"text","text":"q"}]},
				{"role":"assistant","content":[
					{"type":"thinking","thinking":"hm","signature":""},
					{"type":"tool_use","id":"c1","name":"f","input":{}}]}]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertJSON(t, encode(t, ask(tc.input...)),
				`{"model":"m","max_tokens":64,"stream":false,"messages":`+tc.want+`}`)
		})
	}
}

// TestReasoningRoundTrip decodes the recorded tool_use reply and sends its output back, as a
// caller continues the conversation, and wants the thinking block and the tool_use block back as
// they came, in one assistant message, ahead of the call's result.
func TestReasoningRoundTrip(t *testing.T) {
	resp, err := decode(string(readTestdata(t, "tool_use.json")))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	input := []protocol.Item{text(protocol.RoleUser, "What is the weather in Paris right now?")}
	input = append(input, resp.Output...)
	input = append(input, output("tF5fMHb3Wb9oLGnjuubi2GFSlR6QPCmG", `{"city":"Paris","celsius":17}`))
	assertJSON(t, encode(t, ask(input...)), `{"model":"m","max_tokens":64,"stream":false,"messages":[
		{"role":"user","content":[{"type":"text","text":"What is the weather in Paris right now?"}]},
		{"role":"assistant","content":[
			{"type":"thinking","thinking":"User asks current weather in Paris. Use function get_weather.","signature":""},
			{"type":"tool_use","id":"tF5fMHb3Wb9oLGnjuubi2GFSlR6QPCmG","name":"get_weather","input":{"city":"Paris"}}]},
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"tF5fMHb3Wb9oLGnjuubi2GFSlR6QPCmG","content":"{\"city\":\"Paris\",\"celsius\":17}"}]}]}`)
}

// TestRedactedRoundTrip decodes a redacted_thinking block and sends it back, its data unchanged.
func TestRedactedRoundTrip(t *testing.T) {
	resp, err := decode(`{"id":"msg_1","content":[{"type":"redacted_thinking","data":"EmwKAhgBEgy3va3pzix/LafPsn4a"},
		{"type":"text","text":"Paris"}],"stop_reason":"end_turn"}`)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	want := protocol.Reasoning{
		ID:               "msg_1/0",
		Status:           protocol.StatusCompleted,
		EncryptedContent: seal(`{"data":"EmwKAhgBEgy3va3pzix/LafPsn4a"}`),
	}
	if !reflect.DeepEqual(resp.Output[0], want) {
		t.Errorf("Output[0] = %#v, want %#v", resp.Output[0], want)
	}
	input := append([]protocol.Item{text(protocol.RoleUser, "q")}, resp.Output...)
	assertJSON(t, encode(t, ask(input...)), `{"model":"m","max_tokens":64,"stream":false,"messages":[
		{"role":"user","content":[{"type":"text","text":"q"}]},
		{"role":"assistant","content":[
			{"type":"redacted_thinking","data":"EmwKAhgBEgy3va3pzix/LafPsn4a"},
			{"type":"text","text":"Paris"}]}]}`)
}

func TestEncodeTools(t *testing.T) {
	weather := protocol.FunctionTool{
		Name:        "get_weather",
		Description: "Get the weather.",
		Parameters:  jsontext.Value(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
		Strict:      true,
	}
	clock := protocol.FunctionTool{Name: "get_time"}
	const weatherJSON = `{"name":"get_weather","description":"Get the weather.",
		"input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":true}`
	const clockJSON = `{"name":"get_time","input_schema":{"type":"object"}}`
	cases := []struct {
		name   string
		choice protocol.ToolChoice
		want   string
	}{
		{"no choice", nil, `"tools":[` + weatherJSON + `,` + clockJSON + `]`},
		{"auto", protocol.ToolChoiceAuto, `"tools":[` + weatherJSON + `,` + clockJSON + `],"tool_choice":{"type":"auto"}`},
		{"required as any", protocol.ToolChoiceRequired, `"tools":[` + weatherJSON + `,` + clockJSON + `],"tool_choice":{"type":"any"}`},
		{"none", protocol.ToolChoiceNone, `"tools":[` + weatherJSON + `,` + clockJSON + `],"tool_choice":{"type":"none"}`},
		{"a named function as tool", protocol.FunctionToolChoice{Name: "get_time"},
			`"tools":[` + weatherJSON + `,` + clockJSON + `],"tool_choice":{"type":"tool","name":"get_time"}`},
		{"allowed_tools, required: the allowed tools alone, mode any",
			protocol.AllowedToolChoice{Tools: []protocol.FunctionToolChoice{{Name: "get_time"}}, Mode: protocol.ToolChoiceRequired},
			`"tools":[` + clockJSON + `],"tool_choice":{"type":"any"}`},
		{"allowed_tools, auto, in the request's order",
			protocol.AllowedToolChoice{Tools: []protocol.FunctionToolChoice{{Name: "get_time"}, {Name: "get_weather"}}, Mode: protocol.ToolChoiceAuto},
			`"tools":[` + weatherJSON + `,` + clockJSON + `],"tool_choice":{"type":"auto"}`},
		{"allowed_tools, none",
			protocol.AllowedToolChoice{Tools: []protocol.FunctionToolChoice{{Name: "get_weather"}}, Mode: protocol.ToolChoiceNone},
			`"tools":[` + weatherJSON + `],"tool_choice":{"type":"none"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := ask(text(protocol.RoleUser, "q"))
			req.Tools = []protocol.FunctionTool{weather, clock}
			req.ToolChoice = tc.choice
			assertJSON(t, encode(t, req), `{"model":"m","max_tokens":64,"stream":false,
				"messages":[{"role":"user","content":[{"type":"text","text":"q"}]}],`+tc.want+`}`)
		})
	}
}

// TestEncodeOptions covers reasoning effort, the text format, verbosity, and the native tier.
func TestEncodeOptions(t *testing.T) {
	schema := jsontext.Value(`{"type":"object","properties":{"capital":{"type":"string"}},"required":["capital"]}`)
	cases := []struct {
		name string
		set  func(*protocol.Request)
		want string
	}{
		{"effort none as thinking disabled", func(r *protocol.Request) {
			r.Reasoning.Effort = protocol.ReasoningEffortNone
		}, `"thinking":{"type":"disabled"}`},
		{"effort low as adaptive thinking and output_config.effort", func(r *protocol.Request) {
			r.Reasoning.Effort = protocol.ReasoningEffortLow
		}, `"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}`},
		{"effort medium", func(r *protocol.Request) {
			r.Reasoning.Effort = protocol.ReasoningEffortMedium
		}, `"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"}`},
		{"effort high", func(r *protocol.Request) {
			r.Reasoning.Effort = protocol.ReasoningEffortHigh
		}, `"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}`},
		{"effort xhigh", func(r *protocol.Request) {
			r.Reasoning.Effort = protocol.ReasoningEffortXHigh
		}, `"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}`},
		{"a JSON schema as output_config.format, its name, description, and strict dropped", func(r *protocol.Request) {
			r.Text.Format = protocol.JSONSchemaFormat{Name: "capital", Description: "A capital.", Schema: schema, Strict: true}
		}, `"output_config":{"format":{"type":"json_schema","schema":` + string(schema) + `}}`},
		{"effort and a JSON schema in one output_config", func(r *protocol.Request) {
			r.Reasoning.Effort = protocol.ReasoningEffortHigh
			r.Text.Format = protocol.JSONSchemaFormat{Name: "capital", Schema: schema}
		}, `"thinking":{"type":"adaptive"},"output_config":{"effort":"high","format":{"type":"json_schema","schema":` + string(schema) + `}}`},
		{"plain text sends nothing", func(r *protocol.Request) {
			r.Text.Format = protocol.PlainTextFormat{}
		}, ``},
		{"verbosity dropped", func(r *protocol.Request) {
			r.Text.Verbosity = protocol.VerbosityHigh
		}, ``},
		{"native members merged into the top level", func(r *protocol.Request) {
			r.Native = jsontext.Value(`{"temperature":0,"top_k":40,"chat_template_kwargs":{"reasoning_effort":"low"}}`)
		}, `"temperature":0,"top_k":40,"chat_template_kwargs":{"reasoning_effort":"low"}`},
		{"a native member the typed fields leave unset, such as thinking", func(r *protocol.Request) {
			r.Native = jsontext.Value(`{"thinking":{"type":"enabled","budget_tokens":1024}}`)
		}, `"thinking":{"type":"enabled","budget_tokens":1024}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := ask(text(protocol.RoleUser, "q"))
			tc.set(req)
			want := `{"model":"m","max_tokens":64,"stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"q"}]}]`
			if tc.want != "" {
				want += "," + tc.want
			}
			assertJSON(t, encode(t, req), want+"}")
		})
	}
}
