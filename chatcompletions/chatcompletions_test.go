package chatcompletions_test

import (
	"encoding/json/jsontext"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/chatcompletions"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/clienttest"
)

// okReply is a reply that decodes to a completed response.
const okReply = `{"choices":[{"message":{"role":"assistant","content":"Paris"},"finish_reason":"stop"}]}`

// respond sends req through a Chat Completions client to a server answering reply, and returns
// the server, which holds what arrived on the wire, and Respond's results.
func respond(t *testing.T, reply string, req *client.Request) (*clienttest.Server, *client.Response, error) {
	t.Helper()
	srv := clienttest.NewServer(t, http.StatusOK, reply)
	c, err := client.New(client.Config{
		BaseURL: srv.URL + "/v1",
		HTTP:    http.DefaultClient,
		Format:  chatcompletions.New(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := c.Respond(t.Context(), req)
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

func text(role client.Role, s string) client.Message {
	return client.Message{Role: role, Content: []client.ContentPart{client.InputText{Text: s}}}
}

func TestFold(t *testing.T) {
	cases := []struct {
		name string
		req  *client.Request
		want string
	}{
		{
			name: "single input_text as a string",
			req:  &client.Request{Model: "m", Input: []client.Item{text(client.RoleUser, "hi")}},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "instructions as a leading system message",
			req: &client.Request{
				Model:        "m",
				Instructions: "Answer in one word.",
				Input:        []client.Item{text(client.RoleUser, "Capital of France?")},
			},
			want: `{"model":"m","messages":[
				{"role":"system","content":"Answer in one word."},
				{"role":"user","content":"Capital of France?"}]}`,
		},
		{
			name: "several parts as an array",
			req: &client.Request{Model: "m", Input: []client.Item{client.Message{
				Role:    client.RoleUser,
				Content: []client.ContentPart{client.InputText{Text: "one"}, client.InputText{Text: "two"}},
			}}},
			want: `{"model":"m","messages":[{"role":"user","content":[
				{"type":"text","text":"one"},{"type":"text","text":"two"}]}]}`,
		},
		{
			name: "each message its role",
			req: &client.Request{Model: "m", Input: []client.Item{
				text(client.RoleSystem, "s"),
				text(client.RoleDeveloper, "d"),
				text(client.RoleUser, "u"),
				client.Message{Role: client.RoleAssistant, Content: []client.ContentPart{client.OutputText{Text: "a"}}},
				text(client.RoleUser, "u2"),
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
			req: &client.Request{Model: "m", Input: []client.Item{
				client.Message{Role: client.RoleAssistant, Content: []client.ContentPart{
					client.OutputText{Text: "Partly:"}, client.Refusal{Refusal: "not that"},
				}},
				text(client.RoleUser, "ok"),
			}},
			want: `{"model":"m","messages":[
				{"role":"assistant","content":[{"type":"text","text":"Partly:"},{"type":"refusal","refusal":"not that"}]},
				{"role":"user","content":"ok"}]}`,
		},
		{
			name: "single refusal as an array",
			req: &client.Request{Model: "m", Input: []client.Item{
				client.Message{Role: client.RoleAssistant, Content: []client.ContentPart{client.Refusal{Refusal: "no"}}},
			}},
			want: `{"model":"m","messages":[{"role":"assistant","content":[{"type":"refusal","refusal":"no"}]}]}`,
		},
		{
			name: "max_output_tokens as max_completion_tokens",
			req:  &client.Request{Model: "m", Input: []client.Item{text(client.RoleUser, "hi")}, MaxOutputTokens: 64},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":64}`,
		},
		{
			name: "native members at the top level",
			req: &client.Request{
				Model:  "m",
				Input:  []client.Item{text(client.RoleUser, "hi")},
				Native: jsontext.Value(`{"temperature":0.2,"chat_template_kwargs":{"reasoning_effort":"low"}}`),
			},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],
				"temperature":0.2,"chat_template_kwargs":{"reasoning_effort":"low"}}`,
		},
		{
			name: "native member the typed fields leave unset",
			req: &client.Request{
				Model:  "m",
				Input:  []client.Item{text(client.RoleUser, "hi")},
				Native: jsontext.Value(`{"max_completion_tokens":32}`),
			},
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":32}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, err := respond(t, okReply, tc.req)
			if err != nil {
				t.Fatalf("Respond: %v", err)
			}
			got := srv.Only(t)
			if got.Path != "/v1/chat/completions" {
				t.Errorf("path = %q, want /v1/chat/completions", got.Path)
			}
			assertJSON(t, got.Body, tc.want)
		})
	}
}

func TestFoldRefusesNativeClash(t *testing.T) {
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
			srv, resp, err := respond(t, okReply, &client.Request{
				Model:           "m",
				Input:           []client.Item{text(client.RoleUser, "hi")},
				MaxOutputTokens: tc.max,
				Native:          jsontext.Value(tc.native),
			})
			if err == nil {
				t.Fatalf("Respond = %v, nil; want a clash error", resp)
			}
			want := "native member " + tc.member + " clashes"
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Respond error = %q, want it to contain %q", err, want)
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}

func TestUnfoldFinishReason(t *testing.T) {
	cases := []struct {
		reason  string
		status  client.Status
		details *client.IncompleteDetails
	}{
		{"stop", client.StatusCompleted, nil},
		{"tool_calls", client.StatusCompleted, nil},
		{"length", client.StatusIncomplete, &client.IncompleteDetails{Reason: client.ReasonMaxOutputTokens}},
		{"content_filter", client.StatusIncomplete, &client.IncompleteDetails{Reason: client.ReasonContentFilter}},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			reply := `{"choices":[{"message":{"role":"assistant","content":"Paris"},"finish_reason":"` + tc.reason + `"}]}`
			_, resp, err := respond(t, reply, &client.Request{Model: "m", Input: []client.Item{text(client.RoleUser, "hi")}})
			if err != nil {
				t.Fatalf("Respond: %v", err)
			}
			if resp.Status != tc.status {
				t.Errorf("Status = %q, want %q", resp.Status, tc.status)
			}
			if !reflect.DeepEqual(resp.IncompleteDetails, tc.details) {
				t.Errorf("IncompleteDetails = %+v, want %+v", resp.IncompleteDetails, tc.details)
			}
			want := []client.Item{client.Message{
				Status:  tc.status,
				Role:    client.RoleAssistant,
				Content: []client.ContentPart{client.OutputText{Text: "Paris"}},
			}}
			if !reflect.DeepEqual(resp.Output, want) {
				t.Errorf("Output = %#v, want %#v", resp.Output, want)
			}
		})
	}
}

func TestUnfold(t *testing.T) {
	req := &client.Request{Model: "m", Input: []client.Item{text(client.RoleUser, "hi")}}
	cases := []struct {
		name  string
		reply string
		want  *client.Response
	}{
		{
			name: "llama.cpp reply with reasoning_content and usage",
			reply: `{"choices":[{"finish_reason":"stop","index":0,"message":{"role":"assistant",
				"content":"Paris","reasoning_content":"The user asks."}}],
				"created":1793041885,"model":"m","object":"chat.completion",
				"usage":{"completion_tokens":20,"prompt_tokens":83,"total_tokens":103,
					"prompt_tokens_details":{"cached_tokens":56}},
				"id":"chatcmpl-1","timings":{"prompt_n":27}}`,
			want: &client.Response{
				Output: []client.Item{client.Message{
					Status:  client.StatusCompleted,
					Role:    client.RoleAssistant,
					Content: []client.ContentPart{client.OutputText{Text: "Paris"}},
				}},
				Status: client.StatusCompleted,
				Usage:  client.Usage{InputTokens: 83, OutputTokens: 20, TotalTokens: 103},
			},
		},
		{
			name: "reasoning tokens",
			reply: `{"choices":[{"message":{"content":"Paris"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":5,"completion_tokens":30,"total_tokens":35,
					"completion_tokens_details":{"reasoning_tokens":28}}}`,
			want: &client.Response{
				Output: []client.Item{client.Message{
					Status:  client.StatusCompleted,
					Role:    client.RoleAssistant,
					Content: []client.ContentPart{client.OutputText{Text: "Paris"}},
				}},
				Status: client.StatusCompleted,
				Usage: client.Usage{
					InputTokens: 5, OutputTokens: 30, TotalTokens: 35,
					OutputTokensDetails: client.OutputTokensDetails{ReasoningTokens: 28},
				},
			},
		},
		{
			name:  "refusal",
			reply: `{"choices":[{"message":{"content":null,"refusal":"I can't help with that."},"finish_reason":"stop"}]}`,
			want: &client.Response{
				Output: []client.Item{client.Message{
					Status:  client.StatusCompleted,
					Role:    client.RoleAssistant,
					Content: []client.ContentPart{client.Refusal{Refusal: "I can't help with that."}},
				}},
				Status: client.StatusCompleted,
			},
		},
		{
			name:  "no content gives no message",
			reply: `{"choices":[{"message":{"content":"","reasoning_content":"The user"},"finish_reason":"length"}]}`,
			want: &client.Response{
				Status:            client.StatusIncomplete,
				IncompleteDetails: &client.IncompleteDetails{Reason: client.ReasonMaxOutputTokens},
			},
		},
		{
			name: "first choice only",
			reply: `{"choices":[{"message":{"content":"one"},"finish_reason":"stop"},
				{"message":{"content":"two"},"finish_reason":"length"}]}`,
			want: &client.Response{
				Output: []client.Item{client.Message{
					Status:  client.StatusCompleted,
					Role:    client.RoleAssistant,
					Content: []client.ContentPart{client.OutputText{Text: "one"}},
				}},
				Status: client.StatusCompleted,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, resp, err := respond(t, tc.reply, req)
			if err != nil {
				t.Fatalf("Respond: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("Respond = %#v\nwant      %#v", resp, tc.want)
			}
		})
	}
}

func TestUnfoldRefusesReply(t *testing.T) {
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
			_, resp, err := respond(t, tc.reply, &client.Request{Model: "m", Input: []client.Item{text(client.RoleUser, "hi")}})
			if err == nil {
				t.Fatalf("Respond = %v, nil; want an error containing %q", resp, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Respond error = %q, want it to contain %q", err, tc.want)
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
			c, err := client.New(client.Config{BaseURL: srv.URL, HTTP: http.DefaultClient, Format: chatcompletions.New()})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = c.Respond(t.Context(), &client.Request{Model: "m", Input: []client.Item{text(client.RoleUser, "hi")}})
			apiErr, ok := errors.AsType[*client.APIError](err)
			if !ok {
				t.Fatalf("Respond error = %v, want an *APIError", err)
			}
			if apiErr.Message != tc.message || apiErr.Type != tc.typ || apiErr.Code != tc.code {
				t.Errorf("APIError = {%q %q %q}, want {%q %q %q}",
					apiErr.Message, apiErr.Type, apiErr.Code, tc.message, tc.typ, tc.code)
			}
		})
	}
}
