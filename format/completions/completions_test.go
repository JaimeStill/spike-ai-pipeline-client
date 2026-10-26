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
