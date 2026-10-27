package messages_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/format/messages"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/clienttest"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// The tests here drive the format through client.Generate, for what it makes of the request on
// the wire and of a non-2xx reply.

// newClient returns a Client of the server at base speaking the messages format, with token.
func newClient(t *testing.T, base string, token client.TokenSource) *client.Client {
	t.Helper()
	c, err := client.New(client.Config{BaseURL: base, HTTP: http.DefaultClient, Format: messages.New(), Token: token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestGeneratePostsToMessagesWithVersionAndBearer(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, string(readTestdata(t, "conversation.json")))
	c := newClient(t, srv.URL+"/v1", func(context.Context) (string, error) { return "secret", nil })
	if _, err := c.Generate(t.Context(), ask(text(protocol.RoleUser, "hi"))); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	r := srv.Only(t)
	if r.Path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", r.Path)
	}
	for k, want := range map[string]string{"Anthropic-Version": "2026-10-21", "Authorization": "Bearer secret"} {
		if got := r.Header.Values(k); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %q, want [%q]", k, got, want)
		}
	}
	assertJSON(t, r.Body, `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
}

func TestGenerateRefusesNoMaxOutputTokensBeforeTheToken(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, string(readTestdata(t, "conversation.json")))
	called := false
	c := newClient(t, srv.URL, func(context.Context) (string, error) { called = true; return "secret", nil })
	req := ask(text(protocol.RoleUser, "hi"))
	req.MaxOutputTokens = 0
	if _, err := c.Generate(t.Context(), req); err == nil {
		t.Fatal("Generate = nil error, want the request refused")
	}
	if called {
		t.Error("the token source was called for a refused request")
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
	}
}

// TestGenerateAPIError covers the error bodies of both shapes a Messages endpoint answers with,
// each an *APIError of the body's message and type.
func TestGenerateAPIError(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		body        string
		message     string
		typ         string
		code        string
		errorString string
	}{
		{"llama.cpp's", http.StatusBadRequest, string(readTestdata(t, "model_not_found.json")),
			"model 'spike-ai-pipeline/no-such-model' not found", "invalid_request_error", "400",
			"400 Bad Request: model 'spike-ai-pipeline/no-such-model' not found (invalid_request_error, 400)"},
		{"Anthropic's", http.StatusTooManyRequests,
			`{"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded your per-minute rate limit"}}`,
			"Number of request tokens has exceeded your per-minute rate limit", "rate_limit_error", "",
			"429 Too Many Requests: Number of request tokens has exceeded your per-minute rate limit (rate_limit_error)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, tc.status, tc.body)
			_, err := newClient(t, srv.URL, nil).Generate(t.Context(), ask(text(protocol.RoleUser, "hi")))
			var apiErr *client.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Generate error = %v, want an *APIError", err)
			}
			if apiErr.Status != tc.status || apiErr.Message != tc.message || apiErr.Type != tc.typ || apiErr.Code != tc.code {
				t.Errorf("APIError = {%d %q %q %q}, want {%d %q %q %q}",
					apiErr.Status, apiErr.Message, apiErr.Type, apiErr.Code, tc.status, tc.message, tc.typ, tc.code)
			}
			if got := apiErr.Error(); got != tc.errorString {
				t.Errorf("Error() = %q, want %q", got, tc.errorString)
			}
		})
	}
}
