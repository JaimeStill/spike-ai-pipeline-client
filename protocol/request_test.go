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
