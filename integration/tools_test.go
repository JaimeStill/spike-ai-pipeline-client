//go:build integration

package integration_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// getWeather is a function tool the weather question calls.
var getWeather = protocol.FunctionTool{
	Name:        "get_weather",
	Description: "Get the current temperature in a city, in degrees Celsius.",
	Parameters: jsontext.Value(`{"type":"object",
		"properties":{"city":{"type":"string","description":"The city's name."}},
		"required":["city"]}`),
}

// getTime is a second function tool, for the allowed_tools choice to narrow to.
var getTime = protocol.FunctionTool{
	Name:        "get_time",
	Description: "Get the current local time in a city.",
	Parameters:  jsontext.Value(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
}

// weatherQuestion is the user message that asks for the weather in Paris.
var weatherQuestion = userText("What is the weather in Paris right now?")

// weatherCall returns the one function_call to get_weather in resp, failing t unless resp is
// completed and holds one, completed, with arguments that are a JSON object.
func weatherCall(t *testing.T, resp *protocol.Response) protocol.FunctionCall {
	t.Helper()
	if resp.Status != protocol.StatusCompleted {
		t.Fatalf("Status = %q (%+v), want completed", resp.Status, resp.IncompleteDetails)
	}
	var calls []protocol.FunctionCall
	for _, it := range resp.Output {
		if c, ok := it.(protocol.FunctionCall); ok {
			calls = append(calls, c)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("Output has %d function calls, want 1: %#v", len(calls), resp.Output)
	}
	c := calls[0]
	if c.Name != getWeather.Name || c.CallID == "" || c.Status != protocol.StatusCompleted {
		t.Fatalf("FunctionCall = %#v, want a completed call of %s with a call id", c, getWeather.Name)
	}
	if args := jsontext.Value(c.Arguments); args.Kind() != jsontext.KindBeginObject || !args.IsValid() {
		t.Fatalf("FunctionCall arguments %q aren't a JSON object", c.Arguments)
	}
	return c
}

func TestFunctionCallRequired(t *testing.T) {
	eachFormat(t, func(t *testing.T, c *client.Client) {
		resp, err := c.Generate(requestContext(t), &protocol.Request{
			Model:           chatModel,
			Input:           []protocol.Item{weatherQuestion},
			Tools:           []protocol.FunctionTool{getWeather},
			ToolChoice:      protocol.ToolChoiceRequired,
			MaxOutputTokens: chatBound,
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		call := weatherCall(t, resp)
		t.Logf("function_call %s(%s), call_id %q", call.Name, call.Arguments, call.CallID)
	})
}

// TestFunctionCallRoundTrip sends a call's output back after the response's own output items,
// and wants an answer that uses it.
func TestFunctionCallRoundTrip(t *testing.T) {
	eachFormat(t, func(t *testing.T, c *client.Client) {
		req := &protocol.Request{
			Model:           chatModel,
			Input:           []protocol.Item{weatherQuestion},
			Tools:           []protocol.FunctionTool{getWeather},
			ToolChoice:      protocol.ToolChoiceRequired,
			MaxOutputTokens: chatBound,
		}
		resp, err := c.Generate(requestContext(t), req)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		call := weatherCall(t, resp)
		req.Input = append(req.Input, resp.Output...)
		req.Input = append(req.Input, protocol.FunctionCallOutput{
			CallID: call.CallID,
			Output: `{"city":"Paris","celsius":17}`,
		})
		req.ToolChoice = protocol.ToolChoiceAuto
		resp, err = c.Generate(requestContext(t), req)
		if err != nil {
			t.Fatalf("Generate with the call's output: %v", err)
		}
		text := completedText(t, resp)
		t.Logf("answer %q", text)
		if !strings.Contains(text, "17") {
			t.Errorf("the answer %q doesn't use the tool's temperature, 17", text)
		}
	})
}

// TestNamedToolChoices records what the endpoint does with a named function, an allowed_tools,
// and a none tool choice, and passes either way: llama.cpp b11669 treats the named and
// allowed_tools choices as auto on Chat Completions and Open Responses, and on Messages treats a
// named tool as any and drops none, the tools still live. Each asks a question that needs no tool, so a call shows
// a named choice was honored, and none was not.
func TestNamedToolChoices(t *testing.T) {
	cases := []struct {
		name   string
		choice protocol.ToolChoice
	}{
		{"named function get_weather", protocol.FunctionToolChoice{Name: getWeather.Name}},
		{"allowed_tools get_weather, required", protocol.AllowedToolChoice{
			Tools: []protocol.FunctionToolChoice{{Name: getWeather.Name}},
			Mode:  protocol.ToolChoiceRequired,
		}},
		{"none", protocol.ToolChoiceNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eachFormatOf(t, func(t *testing.T, c *client.Client, f testFormat) {
				resp, err := c.Generate(requestContext(t), &protocol.Request{
					Model:           chatModel,
					Input:           []protocol.Item{userText("What is 2 + 2? Answer with the number alone.")},
					Tools:           []protocol.FunctionTool{getTime, getWeather},
					ToolChoice:      tc.choice,
					MaxOutputTokens: chatBound,
				})
				if err != nil {
					f.record(t, "%s: Generate error %v", tc.name, err)
					return
				}
				var got []string
				for _, it := range resp.Output {
					switch it := it.(type) {
					case protocol.Message:
						for _, p := range it.Content {
							if o, ok := p.(protocol.OutputText); ok {
								got = append(got, fmt.Sprintf("output_text %q", o.Text))
							}
						}
					case protocol.FunctionCall:
						got = append(got, "function_call "+it.Name+"("+it.Arguments+")")
					}
				}
				f.record(t, "%s: status %s, output [%s]", tc.name, resp.Status, strings.Join(got, ", "))
			})
		})
	}
}
