package client_test

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/clienttest"
)

// Hand-built embeddings: standard base64 of little-endian float32s.
const (
	// vecA is 1, -2.5, 0.15625: 0x3f800000, 0xc0200000, 0x3e200000.
	vecA = "AACAPwAAIMAAACA+"
	// vecB is 0 and the largest float32, 0x7f7fffff.
	vecB = "AAAAAP//f38="
)

// The vectors vecA and vecB encode.
var (
	wantA = []float32{1, -2.5, 0.15625}
	wantB = []float32{0, math.MaxFloat32}
)

// embedReply is an embeddings reply of vecA at index 0 and vecB at index 1, in that order.
const embedReply = `{"object":"list","model":"m","usage":{"prompt_tokens":6,"total_tokens":6},"data":[` +
	`{"object":"embedding","index":0,"embedding":"` + vecA + `"},` +
	`{"object":"embedding","index":1,"embedding":"` + vecB + `"}]}`

// two is a valid request of two inputs.
func two() *client.EmbeddingRequest {
	return &client.EmbeddingRequest{Model: "m", Input: []string{"a", "b"}}
}

func TestEmbedWireRequest(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, embedReply)
	c := newClient(t, client.Config{
		BaseURL: srv.URL + "/v1/",
		Query:   url.Values{"autoload": {"false"}},
		Token:   func(context.Context) (string, error) { return "secret", nil },
	})
	if _, err := c.Embed(t.Context(), &client.EmbeddingRequest{Model: "m", Input: []string{"a", ""}}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	got := srv.Only(t)
	if got.Method != http.MethodPost || got.Path != "/v1/embeddings" || got.RawQuery != "autoload=false" {
		t.Errorf("request = %s %s?%s, want POST /v1/embeddings?autoload=false", got.Method, got.Path, got.RawQuery)
	}
	for k, want := range map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "application/json",
		"Authorization": "Bearer secret",
	} {
		if v := got.Header.Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	// The empty input goes out as it is: llama.cpp b11669 embeds it.
	if want := `{"model":"m","input":["a",""],"encoding_format":"base64"}`; string(got.Body) != want {
		t.Errorf("body = %s\nwant   %s", got.Body, want)
	}
}

func TestEmbedMergesNative(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, embedReply)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	req := two()
	req.Native = []byte(`{"dimensions":256}`)
	if _, err := c.Embed(t.Context(), req); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if got, want := string(srv.Only(t).Body), `{"model":"m","input":["a","b"],"encoding_format":"base64","dimensions":256}`; got != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}
}

func TestEmbedDecodesVectors(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, embedReply)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	e, err := c.Embed(t.Context(), two())
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(e.Vectors) != 2 || !slices.Equal(e.Vectors[0], wantA) || !slices.Equal(e.Vectors[1], wantB) {
		t.Errorf("Vectors = %v, want [%v %v]", e.Vectors, wantA, wantB)
	}
	if want := (client.EmbeddingUsage{PromptTokens: 6, TotalTokens: 6}); e.Usage != want {
		t.Errorf("Usage = %+v, want %+v", e.Usage, want)
	}
}

// TestEmbedOrdersByIndex covers a reply whose items arrive out of input order: each vector goes
// where its index places it.
func TestEmbedOrdersByIndex(t *testing.T) {
	reply := `{"data":[{"index":1,"embedding":"` + vecB + `"},{"index":0,"embedding":"` + vecA + `"}]}`
	srv := clienttest.NewServer(t, http.StatusOK, reply)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	e, err := c.Embed(t.Context(), two())
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(e.Vectors) != 2 || !slices.Equal(e.Vectors[0], wantA) || !slices.Equal(e.Vectors[1], wantB) {
		t.Errorf("Vectors = %v, want [%v %v]", e.Vectors, wantA, wantB)
	}
}

func TestEmbedRefusesReply(t *testing.T) {
	item := func(index, embedding string) string {
		return `{"index":` + index + `,"embedding":"` + embedding + `"}`
	}
	data := func(items ...string) string { return `{"data":[` + strings.Join(items, ",") + `]}` }
	cases := []struct {
		name  string
		reply string
		want  string
	}{
		{"too few", data(item("0", vecA)), "1 embeddings for 2 inputs"},
		{"too many", data(item("0", vecA), item("1", vecB), item("2", vecB)), "3 embeddings for 2 inputs"},
		{"index past the inputs", data(item("0", vecA), item("2", vecB)), "index 2, outside the 2 inputs"},
		{"negative index", data(item("-1", vecA), item("1", vecB)), "index -1, outside the 2 inputs"},
		{"repeated index", data(item("1", vecA), item("1", vecB)), "repeats index 1"},
		{"partial float32", data(item("0", vecA), item("1", "AACA")), "embedding of 3 bytes isn't whole float32s"},
		{"not base64", data(item("0", vecA), item("1", "!!!!")), "isn't base64"},
		{"float array", `{"data":[{"index":0,"embedding":[1.0]},{"index":1,"embedding":[2.0]}]}`, "decode response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, http.StatusOK, tc.reply)
			c := newClient(t, client.Config{BaseURL: srv.URL})
			e, err := c.Embed(t.Context(), two())
			if err == nil {
				t.Fatalf("Embed = %+v, nil; want an error containing %q", e, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Embed error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestEmbedRefusesRequest covers what Embed refuses, and when: before the token source is called
// or anything is sent.
func TestEmbedRefusesRequest(t *testing.T) {
	cases := []struct {
		name string
		req  *client.EmbeddingRequest
		want string
	}{
		{"nil", nil, "no request"},
		{"no model", &client.EmbeddingRequest{Input: []string{"a"}}, "no model"},
		{"no input", &client.EmbeddingRequest{Model: "m"}, "no input"},
		{"native not an object", &client.EmbeddingRequest{Model: "m", Input: []string{"a"}, Native: []byte(`[1]`)},
			"isn't a JSON object"},
		{"native with a repeated member", &client.EmbeddingRequest{Model: "m", Input: []string{"a"},
			Native: []byte(`{"user":"a","user":"b"}`)}, "isn't a JSON object with unique member names"},
		{"native clash", &client.EmbeddingRequest{Model: "m", Input: []string{"a"},
			Native: []byte(`{"encoding_format":"float"}`)}, "native member encoding_format clashes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, http.StatusOK, embedReply)
			called := false
			c := newClient(t, client.Config{
				BaseURL: srv.URL,
				Token:   func(context.Context) (string, error) { called = true; return "", nil },
			})
			e, err := c.Embed(t.Context(), tc.req)
			if err == nil {
				t.Fatalf("Embed = %+v, nil; want an error containing %q", e, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Embed error = %q, want it to contain %q", err, tc.want)
			}
			if called {
				t.Error("the token source was called")
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}

// TestEmbedAPIError covers a non-2xx reply: an *APIError with the OpenAI-style error body's
// details, whatever format the Client speaks. What the body's shapes give is format's test.
func TestEmbedAPIError(t *testing.T) {
	reply := `{"error":{"code":400,"message":"\"input\" must not be empty","type":"invalid_request_error"}}`
	srv := clienttest.NewServer(t, http.StatusBadRequest, reply)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	e, err := c.Embed(t.Context(), two())
	apiErr, ok := errors.AsType[*client.APIError](err)
	if !ok {
		t.Fatalf("Embed = %+v, %v; want an *APIError", e, err)
	}
	if apiErr.Status != http.StatusBadRequest || apiErr.Message != `"input" must not be empty` ||
		apiErr.Type != "invalid_request_error" || apiErr.Code != "400" || string(apiErr.Body) != reply {
		t.Errorf("APIError = %+v, want 400 with the body's message, type, and code", apiErr)
	}
}

func TestEmbedTokenErrorSendsNothing(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, embedReply)
	errExpired := errors.New("token expired")
	c := newClient(t, client.Config{
		BaseURL: srv.URL,
		Token:   func(context.Context) (string, error) { return "", errExpired },
	})
	_, err := c.Embed(t.Context(), two())
	if !errors.Is(err, errExpired) {
		t.Errorf("Embed error = %v, want it to wrap %v", err, errExpired)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
	}
}

func TestEmbedWrapsCancelledContext(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, embedReply)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Embed(ctx, two())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Embed error = %v, want it to wrap context.Canceled", err)
	}
}
