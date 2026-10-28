package responses_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// weatherTool is a function tool of every field set, strict.
var weatherTool = protocol.FunctionTool{
	Name:        "get_weather",
	Description: "Get the weather.",
	Parameters:  jsontext.Value(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	Strict:      true,
}

// weatherToolJSON is weatherTool on the wire.
const weatherToolJSON = `{"type":"function","name":"get_weather","description":"Get the weather.",
	"parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":true}`

// userHi is the input every case here opens with, on the wire.
const userHi = `{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}`

// withTools is a request of a user's "hi" with tools and choice.
func withTools(choice protocol.ToolChoice, tools ...protocol.FunctionTool) *protocol.Request {
	return &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")}, Tools: tools, ToolChoice: choice}
}

func TestEncodeTools(t *testing.T) {
	timeTool := protocol.FunctionTool{Name: "get_time"}
	cases := []struct {
		name string
		req  *protocol.Request
		want string
	}{
		{
			// strict false is sent, as llama.cpp's router defaults a tool's strict to true.
			name: "tools of every field, and of a name alone, strict always sent",
			req:  withTools(nil, weatherTool, timeTool),
			want: `{"model":"m",` + stateless + `,"input":[` + userHi + `],"tools":[` + weatherToolJSON + `,
				{"type":"function","name":"get_time","strict":false}]}`,
		},
		{
			name: "tool choice auto",
			req:  withTools(protocol.ToolChoiceAuto, weatherTool),
			want: `{"model":"m",` + stateless + `,"input":[` + userHi + `],"tools":[` + weatherToolJSON + `],"tool_choice":"auto"}`,
		},
		{
			name: "tool choice none",
			req:  withTools(protocol.ToolChoiceNone, weatherTool),
			want: `{"model":"m",` + stateless + `,"input":[` + userHi + `],"tools":[` + weatherToolJSON + `],"tool_choice":"none"}`,
		},
		{
			name: "tool choice required",
			req:  withTools(protocol.ToolChoiceRequired, weatherTool),
			want: `{"model":"m",` + stateless + `,"input":[` + userHi + `],"tools":[` + weatherToolJSON + `],"tool_choice":"required"}`,
		},
		{
			name: "tool choice of a named function",
			req:  withTools(protocol.FunctionToolChoice{Name: "get_weather"}, weatherTool),
			want: `{"model":"m",` + stateless + `,"input":[` + userHi + `],"tools":[` + weatherToolJSON + `],
				"tool_choice":{"type":"function","name":"get_weather"}}`,
		},
		{
			// Every tool goes out; the choice narrows them, as the spec has it.
			name: "tool choice of allowed tools, with its mode",
			req: withTools(protocol.AllowedToolChoice{
				Tools: []protocol.FunctionToolChoice{{Name: "get_weather"}},
				Mode:  protocol.ToolChoiceRequired,
			}, weatherTool, timeTool),
			want: `{"model":"m",` + stateless + `,"input":[` + userHi + `],"tools":[` + weatherToolJSON + `,
				{"type":"function","name":"get_time","strict":false}],
				"tool_choice":{"type":"allowed_tools","tools":[{"type":"function","name":"get_weather"}],"mode":"required"}}`,
		},
		{
			name: "tool choice of allowed tools, mode none",
			req: withTools(protocol.AllowedToolChoice{
				Tools: []protocol.FunctionToolChoice{{Name: "get_weather"}, {Name: "get_time"}},
				Mode:  protocol.ToolChoiceNone,
			}, weatherTool, timeTool),
			want: `{"model":"m",` + stateless + `,"input":[` + userHi + `],"tools":[` + weatherToolJSON + `,
				{"type":"function","name":"get_time","strict":false}],
				"tool_choice":{"type":"allowed_tools","tools":[{"type":"function","name":"get_weather"},
					{"type":"function","name":"get_time"}],"mode":"none"}}`,
		},
		{
			name: "reasoning's effort and summary",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")},
				Reasoning: protocol.ReasoningParam{Effort: protocol.ReasoningEffortHigh, Summary: protocol.ReasoningSummaryConcise}},
			want: `{"model":"m",` + stateless + `,"input":[` + userHi + `],"reasoning":{"effort":"high","summary":"concise"}}`,
		},
		{
			name: "reasoning's effort none alone",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")},
				Reasoning: protocol.ReasoningParam{Effort: protocol.ReasoningEffortNone}},
			want: `{"model":"m",` + stateless + `,"input":[` + userHi + `],"reasoning":{"effort":"none"}}`,
		},
		{
			name: "reasoning's summary alone",
			req: &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")},
				Reasoning: protocol.ReasoningParam{Summary: protocol.ReasoningSummaryAuto}},
			want: `{"model":"m",` + stateless + `,"input":[` + userHi + `],"reasoning":{"summary":"auto"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertJSON(t, encode(t, tc.req), tc.want)
		})
	}
}

// TestEncodeFunctionItems covers function_call and function_call_output input items, with and
// without their ids, their statuses never sent, and a call of no arguments sent with "{}".
func TestEncodeFunctionItems(t *testing.T) {
	req := ask(
		text(protocol.RoleUser, "hi"),
		protocol.FunctionCall{ID: "fc_1", Status: protocol.StatusCompleted, CallID: "call_1", Name: "get_weather",
			Arguments: `{"city":"Paris"}`},
		protocol.FunctionCall{CallID: "call_2", Name: "get_time"},
		protocol.FunctionCallOutput{ID: "fco_1", Status: protocol.StatusCompleted, CallID: "call_1", Output: `{"celsius":17}`},
		protocol.FunctionCallOutput{CallID: "call_2", OutputParts: []protocol.ContentPart{
			protocol.InputText{Text: "12:00"}, protocol.InputText{Text: " CET"}}},
		protocol.FunctionCallOutput{CallID: "call_3"},
	)
	assertJSON(t, encode(t, req), `{"model":"m",`+stateless+`,"input":[`+userHi+`,
		{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"},
		{"type":"function_call","call_id":"call_2","name":"get_time","arguments":"{}"},
		{"type":"function_call_output","id":"fco_1","call_id":"call_1","output":"{\"celsius\":17}"},
		{"type":"function_call_output","call_id":"call_2","output":[
			{"type":"input_text","text":"12:00"},{"type":"input_text","text":" CET"}]},
		{"type":"function_call_output","call_id":"call_3","output":""}]}`)
}
