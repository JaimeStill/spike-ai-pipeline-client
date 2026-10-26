package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/chatcompletions"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/clienttest"
)

// okReply is a Chat Completions reply that decodes to a completed response.
const okReply = `{"choices":[{"message":{"role":"assistant","content":"Paris"},"finish_reason":"stop"}]}`

// hello is a minimal valid request.
func hello() *client.Request {
	return &client.Request{
		Model: "m",
		Input: []client.Item{client.Message{
			Role:    client.RoleUser,
			Content: []client.ContentPart{client.InputText{Text: "Capital of France?"}},
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
		cfg.Format = chatcompletions.New()
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewRefusesConfig(t *testing.T) {
	format := chatcompletions.New()
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
	var calls []string
	c := newClient(t, client.Config{
		BaseURL: srv.URL,
		Token: func(context.Context) (string, error) {
			calls = append(calls, "call")
			return "t" + string(rune('0'+len(calls))), nil
		},
	})
	for range 2 {
		if _, err := c.Respond(t.Context(), hello()); err != nil {
			t.Fatalf("Respond: %v", err)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("token source called %d times for 2 requests, want 2", len(calls))
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

func TestRespondAPIError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		want   client.APIError
		text   string
	}{
		{
			name:   "numeric code",
			status: http.StatusBadRequest,
			reply:  `{"error":{"code":400,"message":"model 'x' not found","type":"invalid_request_error"}}`,
			want:   client.APIError{Status: 400, Message: "model 'x' not found", Type: "invalid_request_error", Code: "400"},
			text:   "400 Bad Request: model 'x' not found (invalid_request_error, 400)",
		},
		{
			name:   "string code",
			status: http.StatusTooManyRequests,
			reply:  `{"error":{"message":"slow down","type":"rate_limit","code":"rate_limit_exceeded","param":null}}`,
			want:   client.APIError{Status: 429, Message: "slow down", Type: "rate_limit", Code: "rate_limit_exceeded"},
			text:   "429 Too Many Requests: slow down (rate_limit, rate_limit_exceeded)",
		},
		{
			name:   "message only",
			status: http.StatusInternalServerError,
			reply:  `{"error":{"message":"boom"}}`,
			want:   client.APIError{Status: 500, Message: "boom"},
			text:   "500 Internal Server Error: boom",
		},
		{
			name:   "string error",
			status: http.StatusServiceUnavailable,
			reply:  `{"error":"loading model"}`,
			want:   client.APIError{Status: 503, Message: "loading model"},
			text:   "503 Service Unavailable: loading model",
		},
		{
			name:   "not JSON",
			status: http.StatusBadGateway,
			reply:  "upstream down\n",
			want:   client.APIError{Status: 502},
			text:   "502 Bad Gateway: upstream down",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, tc.status, tc.reply)
			c := newClient(t, client.Config{BaseURL: srv.URL})
			resp, err := c.Respond(t.Context(), hello())
			var apiErr *client.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Respond = %v, %v; want an *APIError", resp, err)
			}
			if apiErr.Status != tc.want.Status || apiErr.Message != tc.want.Message ||
				apiErr.Type != tc.want.Type || apiErr.Code != tc.want.Code {
				t.Errorf("APIError = {%d %q %q %q}, want {%d %q %q %q}",
					apiErr.Status, apiErr.Message, apiErr.Type, apiErr.Code,
					tc.want.Status, tc.want.Message, tc.want.Type, tc.want.Code)
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

func TestRespondRefusesRequest(t *testing.T) {
	user := func(parts ...client.ContentPart) client.Message {
		return client.Message{Role: client.RoleUser, Content: parts}
	}
	with := func(edit func(*client.Request)) *client.Request {
		r := hello()
		edit(r)
		return r
	}
	cases := []struct {
		name string
		req  *client.Request
		want string
	}{
		{"nil", nil, "no request"},
		{"no model", with(func(r *client.Request) { r.Model = "" }), "no model"},
		{"no input", with(func(r *client.Request) { r.Input = nil }), "no input"},
		{"nil item", with(func(r *client.Request) { r.Input = []client.Item{nil} }), "unsupported item"},
		{"pointer item", with(func(r *client.Request) {
			r.Input = []client.Item{&client.Message{Role: client.RoleUser, Content: []client.ContentPart{client.InputText{Text: "hi"}}}}
		}), "unsupported item *client.Message"},
		{"unknown role", with(func(r *client.Request) {
			r.Input = []client.Item{client.Message{Role: "tool", Content: []client.ContentPart{client.InputText{Text: "hi"}}}}
		}), `unknown role "tool"`},
		{"no content", with(func(r *client.Request) { r.Input = []client.Item{user()} }), "has no content"},
		{"nil part", with(func(r *client.Request) { r.Input = []client.Item{user(nil)} }), "unsupported part"},
		{"output part from user", with(func(r *client.Request) {
			r.Input = []client.Item{user(client.OutputText{Text: "hi"})}
		}), "unsupported part client.OutputText"},
		{"refusal from system", with(func(r *client.Request) {
			r.Input = []client.Item{client.Message{Role: client.RoleSystem, Content: []client.ContentPart{client.Refusal{Refusal: "no"}}}}
		}), "unsupported part client.Refusal"},
		{"input part from assistant", with(func(r *client.Request) {
			r.Input = []client.Item{client.Message{Role: client.RoleAssistant, Content: []client.ContentPart{client.InputText{Text: "hi"}}}}
		}), "unsupported part client.InputText"},
		{"negative max output tokens", with(func(r *client.Request) { r.MaxOutputTokens = -1 }), "negative"},
		{"native array", with(func(r *client.Request) { r.Native = []byte(`[1]`) }), "native field"},
		{"native invalid", with(func(r *client.Request) { r.Native = []byte(`{"a":`) }), "native field"},
		{"native duplicate", with(func(r *client.Request) { r.Native = []byte(`{"a":1,"a":2}`) }), "native field"},
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
