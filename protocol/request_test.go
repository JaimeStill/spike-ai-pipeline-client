package protocol_test

import (
	"strings"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// hello is a minimal valid request.
func hello() *protocol.Request {
	return &protocol.Request{
		Model: "m",
		Input: []protocol.Item{protocol.Message{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{protocol.InputText{Text: "Capital of France?"}},
		}},
	}
}

func TestValidateAccepts(t *testing.T) {
	with := func(edit func(*protocol.Request)) *protocol.Request {
		r := hello()
		edit(r)
		return r
	}
	withChoice := func(tc protocol.ToolChoice) *protocol.Request {
		return with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "get_weather"}, {Name: "get_time"}}
			r.ToolChoice = tc
		})
	}
	cases := []struct {
		name string
		req  *protocol.Request
	}{
		{"minimal", hello()},
		{"every role and part", with(func(r *protocol.Request) {
			r.Instructions = "Answer in one word."
			r.MaxOutputTokens = 64
			r.Input = []protocol.Item{
				protocol.Message{Role: protocol.RoleSystem, Content: []protocol.ContentPart{protocol.InputText{Text: "s"}}},
				protocol.Message{Role: protocol.RoleDeveloper, Content: []protocol.ContentPart{protocol.InputText{Text: "d"}}},
				protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.InputText{Text: "u"}}},
				protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
					protocol.OutputText{Text: "a"}, protocol.Refusal{Refusal: "no"},
				}},
			}
		})},
		{"native object", with(func(r *protocol.Request) { r.Native = []byte(`{"temperature":0.2}`) })},
		{"user image and audio", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				protocol.InputText{Text: "What is shown and said?"},
				protocol.InputImage{ImageURL: "https://example.com/shapes.png"},
				protocol.InputImage{ImageURL: protocol.DataURL("image/png", []byte{0x89, 'P', 'N', 'G'}), Detail: protocol.ImageDetailLow},
				protocol.InputImage{ImageURL: "https://example.com/a.png", Detail: protocol.ImageDetailAuto},
				protocol.InputImage{ImageURL: "https://example.com/b.png", Detail: protocol.ImageDetailHigh},
				protocol.InputAudio{Data: []byte("RIFF"), Format: protocol.AudioFormatWAV},
				protocol.InputAudio{Data: []byte("ID3"), Format: protocol.AudioFormatMP3},
			}}}
		})},
		{"function call and output", with(func(r *protocol.Request) {
			r.Input = append(r.Input,
				protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
				protocol.FunctionCall{CallID: "c2", Name: "get_time"},
				protocol.FunctionCallOutput{CallID: "c1", Output: `{"celsius":21}`},
				protocol.FunctionCallOutput{CallID: "c2"},
				protocol.FunctionCallOutput{CallID: "c3", OutputParts: []protocol.ContentPart{
					protocol.InputText{Text: "21"}, protocol.InputText{Text: "celsius"},
				}},
			)
		})},
		{"tools", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{
				{Name: "get_weather", Description: "d", Parameters: []byte(`{"type":"object"}`), Strict: true},
				{Name: "Get-Time_2"},
			}
		})},
		{"tool choice auto", withChoice(protocol.ToolChoiceAuto)},
		{"tool choice none", withChoice(protocol.ToolChoiceNone)},
		{"tool choice required", withChoice(protocol.ToolChoiceRequired)},
		{"tool choice of a named function", withChoice(protocol.FunctionToolChoice{Name: "get_weather"})},
		{"tool choice of allowed tools", withChoice(protocol.AllowedToolChoice{
			Tools: []protocol.FunctionToolChoice{{Name: "get_weather"}, {Name: "get_time"}},
			Mode:  protocol.ToolChoiceRequired,
		})},
		{"tool choice of allowed tools, mode none", withChoice(protocol.AllowedToolChoice{
			Tools: []protocol.FunctionToolChoice{{Name: "get_time"}},
			Mode:  protocol.ToolChoiceNone,
		})},
		{"tool name of 64 characters", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: strings.Repeat("a", 64)}}
		})},
		// Content, which the spec's input reasoning item lacks, is accepted on input and never
		// sent, so a response's Output goes back as it came.
		{"reasoning items, content on input included", with(func(r *protocol.Request) {
			r.Input = append(r.Input,
				protocol.Reasoning{
					ID:               "rs_1",
					Content:          []protocol.ReasoningText{{Text: "The user asks."}},
					Summary:          []protocol.SummaryText{{Text: "Asked."}},
					EncryptedContent: "opaque",
				},
				protocol.Reasoning{},
				protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}}},
			)
		})},
		{"reasoning effort none", with(func(r *protocol.Request) { r.Reasoning.Effort = protocol.ReasoningEffortNone })},
		{"reasoning effort low", with(func(r *protocol.Request) { r.Reasoning.Effort = protocol.ReasoningEffortLow })},
		{"reasoning effort medium", with(func(r *protocol.Request) { r.Reasoning.Effort = protocol.ReasoningEffortMedium })},
		{"reasoning effort high", with(func(r *protocol.Request) { r.Reasoning.Effort = protocol.ReasoningEffortHigh })},
		{"reasoning effort xhigh", with(func(r *protocol.Request) { r.Reasoning.Effort = protocol.ReasoningEffortXHigh })},
		{"plain text format", with(func(r *protocol.Request) { r.Text.Format = protocol.PlainTextFormat{} })},
		{"json_schema format, every field set", with(func(r *protocol.Request) {
			r.Text.Format = protocol.JSONSchemaFormat{
				Name:        "Capital-2_x",
				Description: "A country's capital.",
				Schema:      []byte(`{"type":"object","properties":{"capital":{"type":"string"}}}`),
				Strict:      true,
			}
		})},
		{"json_schema format name of 64 characters", with(func(r *protocol.Request) {
			r.Text.Format = protocol.JSONSchemaFormat{Name: strings.Repeat("a", 64), Schema: []byte(`{}`)}
		})},
		{"verbosity low", with(func(r *protocol.Request) { r.Text.Verbosity = protocol.VerbosityLow })},
		{"verbosity medium", with(func(r *protocol.Request) { r.Text.Verbosity = protocol.VerbosityMedium })},
		{"verbosity high", with(func(r *protocol.Request) { r.Text.Verbosity = protocol.VerbosityHigh })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(); err != nil {
				t.Errorf("Validate = %v, want nil", err)
			}
		})
	}
}

func TestValidateRefuses(t *testing.T) {
	user := func(parts ...protocol.ContentPart) protocol.Message {
		return protocol.Message{Role: protocol.RoleUser, Content: parts}
	}
	image := protocol.InputImage{ImageURL: "https://example.com/shapes.png"}
	audio := protocol.InputAudio{Data: []byte("RIFF"), Format: protocol.AudioFormatWAV}
	with := func(edit func(*protocol.Request)) *protocol.Request {
		r := hello()
		edit(r)
		return r
	}
	cases := []struct {
		name string
		req  *protocol.Request
		want string
	}{
		{"nil", nil, "no request"},
		{"no model", with(func(r *protocol.Request) { r.Model = "" }), "no model"},
		{"no input", with(func(r *protocol.Request) { r.Input = nil }), "no input"},
		{"nil item", with(func(r *protocol.Request) { r.Input = []protocol.Item{nil} }), "unsupported item"},
		{"pointer item", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{&protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.InputText{Text: "hi"}}}}
		}), "unsupported item *protocol.Message"},
		{"unknown role", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: "tool", Content: []protocol.ContentPart{protocol.InputText{Text: "hi"}}}}
		}), `unknown role "tool"`},
		{"unknown role, no content", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: "tool"}}
		}), `unknown role "tool"`},
		{"no content", with(func(r *protocol.Request) { r.Input = []protocol.Item{user()} }), "has no content"},
		{"nil part", with(func(r *protocol.Request) { r.Input = []protocol.Item{user(nil)} }), "unsupported part"},
		{"output part from user", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{user(protocol.OutputText{Text: "hi"})}
		}), "unsupported part protocol.OutputText"},
		{"refusal from system", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: protocol.RoleSystem, Content: []protocol.ContentPart{protocol.Refusal{Refusal: "no"}}}}
		}), "unsupported part protocol.Refusal"},
		{"input part from assistant", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.InputText{Text: "hi"}}}}
		}), "unsupported part protocol.InputText"},
		{"image from system", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: protocol.RoleSystem, Content: []protocol.ContentPart{image}}}
		}), "system message content 0: unsupported part protocol.InputImage"},
		{"image from developer", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: protocol.RoleDeveloper, Content: []protocol.ContentPart{image}}}
		}), "developer message content 0: unsupported part protocol.InputImage"},
		{"image from assistant", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{image}}}
		}), "assistant message content 0: unsupported part protocol.InputImage"},
		{"audio from system", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: protocol.RoleSystem, Content: []protocol.ContentPart{audio}}}
		}), "system message content 0: unsupported part protocol.InputAudio"},
		{"audio from developer", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: protocol.RoleDeveloper, Content: []protocol.ContentPart{audio}}}
		}), "developer message content 0: unsupported part protocol.InputAudio"},
		{"audio from assistant", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{audio}}}
		}), "assistant message content 0: unsupported part protocol.InputAudio"},
		{"image with no URL", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{user(protocol.InputText{Text: "hi"}, protocol.InputImage{Detail: protocol.ImageDetailLow})}
		}), "user message content 1: image has no URL"},
		{"image with unknown detail", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{user(protocol.InputImage{ImageURL: "https://example.com/a.png", Detail: "medium"})}
		}), `image has unknown detail "medium"`},
		{"audio with no data", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{user(protocol.InputAudio{Format: protocol.AudioFormatWAV})}
		}), "audio has no data"},
		{"audio with no format", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{user(protocol.InputAudio{Data: []byte("RIFF")})}
		}), `audio has unknown format ""`},
		{"audio with unknown format", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{user(protocol.InputAudio{Data: []byte("OggS"), Format: "ogg"})}
		}), `audio has unknown format "ogg"`},
		{"pointer image", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{user(&protocol.InputImage{ImageURL: "https://example.com/a.png"})}
		}), "unsupported part *protocol.InputImage"},
		{"function call with no call id", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.FunctionCall{Name: "f"}}
		}), "input 0: function_call has no call id"},
		{"function call with no name", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.FunctionCall{CallID: "c1"}}
		}), "function_call c1 has no name"},
		{"function call with arguments that aren't JSON", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.FunctionCall{CallID: "c1", Name: "f", Arguments: `{"city":`}}
		}), "function_call c1 has arguments that aren't JSON"},
		{"pointer reasoning", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{&protocol.Reasoning{EncryptedContent: "opaque"}}
		}), "unsupported item *protocol.Reasoning"},
		{"unknown reasoning effort", with(func(r *protocol.Request) { r.Reasoning.Effort = "minimal" }),
			`unknown reasoning effort "minimal"`},
		{"pointer function call", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{&protocol.FunctionCall{CallID: "c1", Name: "f"}}
		}), "unsupported item *protocol.FunctionCall"},
		{"function call output with no call id", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.FunctionCallOutput{Output: "21"}}
		}), "function_call_output has no call id"},
		{"function call output with both forms", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.FunctionCallOutput{
				CallID: "c1", Output: "21", OutputParts: []protocol.ContentPart{protocol.InputText{Text: "21"}},
			}}
		}), "function_call_output c1 has both an output string and output parts"},
		{"function call output with an output_text part", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.FunctionCallOutput{
				CallID: "c1", OutputParts: []protocol.ContentPart{protocol.OutputText{Text: "21"}},
			}}
		}), "function_call_output c1 part 0: unsupported part protocol.OutputText"},
		{"function call output with an image part", with(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.FunctionCallOutput{
				CallID: "c1", OutputParts: []protocol.ContentPart{protocol.InputText{Text: "see"}, image},
			}}
		}), "function_call_output c1 part 1: unsupported part protocol.InputImage"},
		{"tool with no name", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "f"}, {Description: "d"}}
		}), "tool 1: tool has no name"},
		{"tool name with a space", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "get weather"}}
		}), `tool name "get weather" isn't`},
		{"tool name of 65 characters", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: strings.Repeat("a", 65)}}
		}), "isn't 1 to 64"},
		{"tool name twice", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "f"}, {Name: "g"}, {Name: "f"}}
		}), `tool 2: tool name "f" is declared twice`},
		{"tool parameters an array", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "f", Parameters: []byte(`[]`)}}
		}), "tool f's parameters aren't a JSON object"},
		{"tool parameters invalid", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "f", Parameters: []byte(`{"type":`)}}
		}), "tool f's parameters aren't a JSON object"},
		{"tool choice of unknown mode", with(func(r *protocol.Request) {
			r.ToolChoice = protocol.ToolChoiceMode("any")
		}), `tool choice: unknown mode "any"`},
		{"tool choice of empty mode", with(func(r *protocol.Request) {
			r.ToolChoice = protocol.ToolChoiceMode("")
		}), `tool choice: unknown mode ""`},
		{"named function undeclared", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "f"}}
			r.ToolChoice = protocol.FunctionToolChoice{Name: "g"}
		}), `tool choice: function "g" isn't one of the request's tools`},
		{"named function with no tools", with(func(r *protocol.Request) {
			r.ToolChoice = protocol.FunctionToolChoice{Name: "f"}
		}), `function "f" isn't one of the request's tools`},
		{"allowed tool undeclared", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "f"}}
			r.ToolChoice = protocol.AllowedToolChoice{
				Tools: []protocol.FunctionToolChoice{{Name: "f"}, {Name: "g"}},
				Mode:  protocol.ToolChoiceAuto,
			}
		}), `allowed_tools tool 1: function "g" isn't one of the request's tools`},
		{"allowed tools empty", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "f"}}
			r.ToolChoice = protocol.AllowedToolChoice{Mode: protocol.ToolChoiceAuto}
		}), "allowed_tools has no tools"},
		{"allowed tools with no mode", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "f"}}
			r.ToolChoice = protocol.AllowedToolChoice{Tools: []protocol.FunctionToolChoice{{Name: "f"}}}
		}), `allowed_tools: unknown mode ""`},
		{"allowed tools of unknown mode", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "f"}}
			r.ToolChoice = protocol.AllowedToolChoice{Tools: []protocol.FunctionToolChoice{{Name: "f"}}, Mode: "any"}
		}), `allowed_tools: unknown mode "any"`},
		{"pointer tool choice", with(func(r *protocol.Request) {
			r.Tools = []protocol.FunctionTool{{Name: "f"}}
			r.ToolChoice = &protocol.FunctionToolChoice{Name: "f"}
		}), "unsupported tool choice *protocol.FunctionToolChoice"},
		{"json_schema format with no name", with(func(r *protocol.Request) {
			r.Text.Format = protocol.JSONSchemaFormat{Schema: []byte(`{}`)}
		}), "text: json_schema format has no name"},
		{"json_schema format name with a space", with(func(r *protocol.Request) {
			r.Text.Format = protocol.JSONSchemaFormat{Name: "a capital", Schema: []byte(`{}`)}
		}), `json_schema format name "a capital" isn't`},
		{"json_schema format name of 65 characters", with(func(r *protocol.Request) {
			r.Text.Format = protocol.JSONSchemaFormat{Name: strings.Repeat("a", 65), Schema: []byte(`{}`)}
		}), "isn't 1 to 64"},
		{"json_schema format with no schema", with(func(r *protocol.Request) {
			r.Text.Format = protocol.JSONSchemaFormat{Name: "capital"}
		}), "json_schema format capital has no schema"},
		{"json_schema format schema an array", with(func(r *protocol.Request) {
			r.Text.Format = protocol.JSONSchemaFormat{Name: "capital", Schema: []byte(`[]`)}
		}), "json_schema format capital's schema isn't a JSON object"},
		{"json_schema format schema invalid", with(func(r *protocol.Request) {
			r.Text.Format = protocol.JSONSchemaFormat{Name: "capital", Schema: []byte(`{"type":`)}
		}), "json_schema format capital's schema isn't a JSON object"},
		{"json_schema format schema with a duplicate member", with(func(r *protocol.Request) {
			r.Text.Format = protocol.JSONSchemaFormat{Name: "capital", Schema: []byte(`{"type":"object","type":"string"}`)}
		}), "json_schema format capital's schema isn't a JSON object"},
		{"pointer text format", with(func(r *protocol.Request) {
			r.Text.Format = &protocol.JSONSchemaFormat{Name: "capital", Schema: []byte(`{}`)}
		}), "text: unsupported format *protocol.JSONSchemaFormat"},
		{"unknown verbosity", with(func(r *protocol.Request) { r.Text.Verbosity = "max" }),
			`text: unknown verbosity "max"`},
		{"negative max output tokens", with(func(r *protocol.Request) { r.MaxOutputTokens = -1 }), "negative"},
		{"native array", with(func(r *protocol.Request) { r.Native = []byte(`[1]`) }), "native field"},
		{"native invalid", with(func(r *protocol.Request) { r.Native = []byte(`{"a":`) }), "native field"},
		{"native duplicate", with(func(r *protocol.Request) { r.Native = []byte(`{"a":1,"a":2}`) }), "native field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if err == nil {
				t.Fatalf("Validate = nil, want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestDataURL(t *testing.T) {
	cases := []struct {
		mediaType string
		data      []byte
		want      string
	}{
		{"image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "data:image/png;base64,iVBORw0KGgo="},
		{"image/jpeg", []byte{0xff, 0xd8, 0xff}, "data:image/jpeg;base64,/9j/"},
		{"image/gif", nil, "data:image/gif;base64,"},
	}
	for _, tc := range cases {
		t.Run(tc.mediaType, func(t *testing.T) {
			if got := protocol.DataURL(tc.mediaType, tc.data); got != tc.want {
				t.Errorf("DataURL(%q, %v) = %q, want %q", tc.mediaType, tc.data, got, tc.want)
			}
		})
	}
}
