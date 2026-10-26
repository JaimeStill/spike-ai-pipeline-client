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
