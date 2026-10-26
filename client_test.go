package client_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/format/completions"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/clienttest"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// okReply is a Chat Completions reply that decodes to a completed response.
const okReply = `{"choices":[{"message":{"role":"assistant","content":"Paris"},"finish_reason":"stop"}]}`

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

// newClient returns a Client of cfg, failing t if New refuses it. cfg's HTTP and Format default
// to http.DefaultClient and Chat Completions.
func newClient(t *testing.T, cfg client.Config) *client.Client {
	t.Helper()
	if cfg.HTTP == nil {
		cfg.HTTP = http.DefaultClient
	}
	if cfg.Format == nil {
		cfg.Format = completions.New()
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewRefusesConfig(t *testing.T) {
	format := completions.New()
	cases := []struct {
		name string
		cfg  client.Config
		want string
	}{
		{"no HTTP client", client.Config{BaseURL: "http://h/v1", Format: format}, "no HTTP client"},
		{"no format", client.Config{BaseURL: "http://h/v1", HTTP: http.DefaultClient}, "no format"},
		{"no base URL", client.Config{HTTP: http.DefaultClient, Format: format}, "no base URL"},
		{"relative base URL", client.Config{BaseURL: "/v1", HTTP: http.DefaultClient, Format: format}, "isn't absolute"},
		{"no host", client.Config{BaseURL: "http:///v1", HTTP: http.DefaultClient, Format: format}, "isn't absolute"},
		{"unparsable", client.Config{BaseURL: "http://h/%zz", HTTP: http.DefaultClient, Format: format}, "base URL"},
		{"query", client.Config{BaseURL: "http://h/v1?autoload=false", HTTP: http.DefaultClient, Format: format}, "has a query"},
		{"empty query", client.Config{BaseURL: "http://h/v1?", HTTP: http.DefaultClient, Format: format}, "has a query"},
		{"fragment", client.Config{BaseURL: "http://h/v1#top", HTTP: http.DefaultClient, Format: format}, "has a fragment"},
		{"scheme", client.Config{BaseURL: "ftp://h/v1", HTTP: http.DefaultClient, Format: format}, "isn't http or https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := client.New(tc.cfg)
			if err == nil {
				t.Fatalf("New = %v, nil; want an error containing %q", c, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("New error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestNewMakesNoRequest(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, okReply)
	called := false
	newClient(t, client.Config{
		BaseURL: srv.URL,
		Token:   func(context.Context) (string, error) { called = true; return "", nil },
	})
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("New sent %d requests, want 0", n)
	}
	if called {
		t.Error("New called the token source")
	}
}

func TestRespondJoinsRouteWithOneSlash(t *testing.T) {
	for base, want := range map[string]string{
		"":     "/chat/completions",
		"/":    "/chat/completions",
		"/v1":  "/v1/chat/completions",
		"/v1/": "/v1/chat/completions",
	} {
		t.Run(base, func(t *testing.T) {
			srv := clienttest.NewServer(t, http.StatusOK, okReply)
			c := newClient(t, client.Config{BaseURL: srv.URL + base})
			if _, err := c.Respond(t.Context(), hello()); err != nil {
				t.Fatalf("Respond: %v", err)
			}
			got := srv.Only(t)
			if got.Method != http.MethodPost || got.Path != want {
				t.Errorf("request = %s %s, want POST %s", got.Method, got.Path, want)
			}
		})
	}
}

func TestRespondCarriesQueryCopiedAtNew(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, okReply)
	query := url.Values{"autoload": {"false"}}
	c := newClient(t, client.Config{BaseURL: srv.URL + "/v1", Query: query})
	query.Set("autoload", "true")
	query.Add("extra", "1")
	if _, err := c.Respond(t.Context(), hello()); err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if got := srv.Only(t).RawQuery; got != "autoload=false" {
		t.Errorf("query = %q, want autoload=false", got)
	}
}

func TestRespondSendsJSONHeaders(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, okReply)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	if _, err := c.Respond(t.Context(), hello()); err != nil {
		t.Fatalf("Respond: %v", err)
	}
	h := srv.Only(t).Header
	for k, want := range map[string]string{"Content-Type": "application/json", "Accept": "application/json"} {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestRespondToken(t *testing.T) {
	cases := []struct {
		name  string
		token client.TokenSource
		want  []string
	}{
		{"bearer", func(context.Context) (string, error) { return "secret", nil }, []string{"Bearer secret"}},
		{"empty token", func(context.Context) (string, error) { return "", nil }, nil},
		{"no source", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, http.StatusOK, okReply)
			c := newClient(t, client.Config{BaseURL: srv.URL, Token: tc.token})
			if _, err := c.Respond(t.Context(), hello()); err != nil {
				t.Fatalf("Respond: %v", err)
			}
			got := srv.Only(t).Header.Values("Authorization")
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("Authorization = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRespondCallsTokenSourceOncePerRequest(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, okReply)
	calls := 0
	c := newClient(t, client.Config{
		BaseURL: srv.URL,
		Token: func(context.Context) (string, error) {
			calls++
			return fmt.Sprintf("t%d", calls), nil
		},
	})
	for range 2 {
		if _, err := c.Respond(t.Context(), hello()); err != nil {
			t.Fatalf("Respond: %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("token source called %d times for 2 requests, want 2", calls)
	}
	reqs := srv.Requests()
	for i, want := range []string{"Bearer t1", "Bearer t2"} {
		if got := reqs[i].Header.Get("Authorization"); got != want {
			t.Errorf("request %d Authorization = %q, want %q", i, got, want)
		}
	}
}

func TestRespondTokenErrorSendsNothing(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, okReply)
	errExpired := errors.New("token expired")
	c := newClient(t, client.Config{
		BaseURL: srv.URL,
		Token:   func(context.Context) (string, error) { return "", errExpired },
	})
	_, err := c.Respond(t.Context(), hello())
	if !errors.Is(err, errExpired) {
		t.Errorf("Respond error = %v, want it to wrap %v", err, errExpired)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
	}
}

// TestRespondAPIError covers what the Client makes of a non-2xx reply: an *APIError with the
// status and the whole body, its text built from the details the Format decoded or, without a
// message, from the start of the body. What the Format decodes is completions' test.
func TestRespondAPIError(t *testing.T) {
	long := strings.Repeat("x", 600)
	cases := []struct {
		name   string
		status int
		reply  string
		text   string
	}{
		{
			name:   "message, type, and code",
			status: http.StatusBadRequest,
			reply:  `{"error":{"code":400,"message":"model 'x' not found","type":"invalid_request_error"}}`,
			text:   "400 Bad Request: model 'x' not found (invalid_request_error, 400)",
		},
		{
			name:   "message only",
			status: http.StatusInternalServerError,
			reply:  `{"error":{"message":"boom"}}`,
			text:   "500 Internal Server Error: boom",
		},
		{
			name:   "no message",
			status: http.StatusBadGateway,
			reply:  "upstream down\n",
			text:   "502 Bad Gateway: upstream down",
		},
		{
			name:   "no message, long body",
			status: http.StatusBadGateway,
			reply:  long,
			text:   "502 Bad Gateway: " + long[:512] + "...",
		},
		{
			name:   "empty body",
			status: http.StatusServiceUnavailable,
			reply:  "",
			text:   "503 Service Unavailable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, tc.status, tc.reply)
			c := newClient(t, client.Config{BaseURL: srv.URL})
			resp, err := c.Respond(t.Context(), hello())
			apiErr, ok := errors.AsType[*client.APIError](err)
			if !ok {
				t.Fatalf("Respond = %v, %v; want an *APIError", resp, err)
			}
			if apiErr.Status != tc.status {
				t.Errorf("APIError.Status = %d, want %d", apiErr.Status, tc.status)
			}
			if string(apiErr.Body) != tc.reply {
				t.Errorf("APIError.Body = %q, want %q", apiErr.Body, tc.reply)
			}
			if got := apiErr.Error(); got != tc.text {
				t.Errorf("APIError.Error() = %q, want %q", got, tc.text)
			}
		})
	}
}

// roundTripper is an http.RoundTripper made of a function.
type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRespondWrapsTransportError(t *testing.T) {
	errDown := errors.New("network down")
	c := newClient(t, client.Config{
		BaseURL: "http://h/v1",
		HTTP: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errDown
		})},
	})
	_, err := c.Respond(t.Context(), hello())
	if !errors.Is(err, errDown) {
		t.Errorf("Respond error = %v, want it to wrap %v", err, errDown)
	}
	if _, ok := errors.AsType[*client.APIError](err); ok {
		t.Errorf("Respond error = %v, an *APIError; want a transport error", err)
	}
}

func TestRespondWrapsCancelledContext(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, okReply)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Respond(ctx, hello())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Respond error = %v, want it to wrap context.Canceled", err)
	}
}

// TestRespondRefusesInvalidRequest covers when Respond validates: before the token source is
// called or anything is sent. Which requests Validate refuses is protocol's test.
func TestRespondRefusesInvalidRequest(t *testing.T) {
	noModel := hello()
	noModel.Model = ""
	cases := []struct {
		name string
		req  *protocol.Request
		want string
	}{
		{"nil", nil, "no request"},
		{"no model", noModel, "no model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, http.StatusOK, okReply)
			called := false
			c := newClient(t, client.Config{
				BaseURL: srv.URL,
				Token:   func(context.Context) (string, error) { called = true; return "", nil },
			})
			resp, err := c.Respond(t.Context(), tc.req)
			if err == nil {
				t.Fatalf("Respond = %v, nil; want an error containing %q", resp, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Respond error = %q, want it to contain %q", err, tc.want)
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
