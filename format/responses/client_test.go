package responses_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/format/responses"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/clienttest"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// The tests here drive the format through client.Generate, for what it makes of the request on
// the wire and of a non-2xx reply.

// newClient returns a Client of the server at base speaking the spec's form of the responses
// format, with token.
func newClient(t *testing.T, base string, token client.TokenSource) *client.Client {
	t.Helper()
	c, err := client.New(client.Config{BaseURL: base, HTTP: http.DefaultClient,
		Format: responses.New(responses.Options{}), Token: token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestGeneratePostsToResponsesWithBearer(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, string(readTestdata(t, "conversation.json")))
	c := newClient(t, srv.URL+"/v1", func(context.Context) (string, error) { return "secret", nil })
	resp, err := c.Generate(t.Context(), ask(text(protocol.RoleUser, "hi")))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Status != protocol.StatusCompleted || len(resp.Output) != 1 {
		t.Errorf("Generate = %+v, want the recorded reply's one completed message", resp)
	}
	r := srv.Only(t)
	if r.Path != "/v1/responses" {
		t.Errorf("path = %q, want /v1/responses", r.Path)
	}
	if got := r.Header.Values("Authorization"); len(got) != 1 || got[0] != "Bearer secret" {
		t.Errorf("Authorization = %q, want [%q]", got, "Bearer secret")
	}
	assertJSON(t, r.Body, `{"model":"m",`+stateless+`,"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
}

// TestGenerateAPIError covers the router's error body, recorded, an *APIError of its message,
// type, and numeric code.
func TestGenerateAPIError(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusBadRequest, string(readTestdata(t, "model_not_found.json")))
	c := newClient(t, srv.URL, nil)
	_, err := c.Generate(t.Context(), ask(text(protocol.RoleUser, "hi")))
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Generate error = %v, want an *APIError", err)
	}
	want := client.APIError{
		Status:  http.StatusBadRequest,
		Message: "model 'spike-ai-pipeline/no-such-model' not found",
		Type:    "invalid_request_error",
		Code:    "400",
	}
	if apiErr.Status != want.Status || apiErr.Message != want.Message || apiErr.Type != want.Type || apiErr.Code != want.Code {
		t.Errorf("APIError = %+v, want %+v", *apiErr, want)
	}
}
