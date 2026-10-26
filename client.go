package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
)

// TokenSource returns the bearer token for one request. The Client calls it once per request, so
// a source whose tokens expire can refresh them. An empty token sends no Authorization header.
type TokenSource func(ctx context.Context) (string, error)

// Config describes the endpoint a Client talks to and the format it speaks there.
type Config struct {
	// BaseURL is the endpoint's API root, such as "http://localhost:8080/v1". Each route, such
	// as "chat/completions", is appended to its path with exactly one slash between them. It is
	// an absolute http or https URL with no query or fragment; Query holds the query.
	BaseURL string
	// Query is added to every request's URL. New copies it, so changing the url.Values after New
	// doesn't change the requests.
	Query url.Values
	// Token authenticates each request as a bearer token. Nil sends no Authorization header.
	Token TokenSource
	// HTTP sends the requests. It is required, and its transport and timeouts are the Client's:
	// the library sets no timeout, retry, or limit of its own.
	HTTP *http.Client
	// Format is the wire format Respond speaks, chosen by its constructor, such as
	// chatcompletions.New(). It is required.
	Format Format
}

// Client makes requests of one endpoint in one wire format. It is safe for concurrent use when
// its TokenSource and Format are.
type Client struct {
	base   url.URL
	query  string
	token  TokenSource
	http   *http.Client
	format Format
}

// New returns a Client for cfg. It makes no request, so it reports only a Config that can't
// describe an endpoint: no HTTP client, no Format, or a BaseURL that isn't an absolute http or
// https URL without a query or fragment.
func New(cfg Config) (*Client, error) {
	if cfg.HTTP == nil {
		return nil, errors.New("client: config has no HTTP client")
	}
	if cfg.Format == nil {
		return nil, errors.New("client: config has no format")
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("client: config has no base URL")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("client: base URL: %w", err)
	}
	switch {
	case !u.IsAbs() || u.Host == "":
		return nil, fmt.Errorf("client: base URL %q isn't absolute", cfg.BaseURL)
	case u.Scheme != "http" && u.Scheme != "https":
		return nil, fmt.Errorf("client: base URL %q isn't http or https", cfg.BaseURL)
	case u.RawQuery != "" || u.ForceQuery:
		return nil, fmt.Errorf("client: base URL %q has a query; set Config.Query instead", cfg.BaseURL)
	case u.Fragment != "" || u.RawFragment != "":
		return nil, fmt.Errorf("client: base URL %q has a fragment", cfg.BaseURL)
	}
	// The query is encoded once, here, so a caller changing its url.Values after New can't
	// change the requests.
	return &Client{
		base:   *u,
		query:  cfg.Query.Encode(),
		token:  cfg.Token,
		http:   cfg.HTTP,
		format: cfg.Format,
	}, nil
}

// Respond sends req in the Client's format and returns the model's response. It validates req,
// and the format encodes it, before the token source is called or anything is sent. A non-2xx
// reply is an *APIError; a transport failure or a cancelled ctx is returned wrapped.
func (c *Client) Respond(ctx context.Context, req *Request) (*Response, error) {
	if err := req.validate(); err != nil {
		return nil, fmt.Errorf("client: respond: %w", err)
	}
	header := make(http.Header)
	body, err := c.format.EncodeRequest(req, header)
	if err != nil {
		return nil, fmt.Errorf("client: respond: %w", err)
	}
	reply, err := c.post(ctx, c.format.Route(), header, body)
	if err != nil {
		return nil, fmt.Errorf("client: respond: %w", err)
	}
	resp, err := c.format.DecodeResponse(reply)
	if err != nil {
		return nil, fmt.Errorf("client: respond: %w", err)
	}
	return resp, nil
}

// endpoint is the URL of route: the base path and route joined by exactly one slash, with the
// Config's query.
func (c *Client) endpoint(route string) string {
	u := c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(route, "/")
	u.RawPath = ""
	u.RawQuery = c.query
	return u.String()
}

// post sends body as a JSON POST to route, with header and the bearer token, and returns the
// reply's body. A non-2xx reply is an *APIError, its details decoded by the Client's format.
func (c *Client) post(ctx context.Context, route string, header http.Header, body []byte) ([]byte, error) {
	token := ""
	if c.token != nil {
		t, err := c.token(ctx)
		if err != nil {
			return nil, fmt.Errorf("token: %w", err)
		}
		token = t
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(route), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	maps.Copy(req.Header, header)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	reply, err := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// A failed read leaves the part of the body that was read, which is still the best
		// account of the error there is.
		e := &APIError{Status: resp.StatusCode, Body: reply}
		e.Message, e.Type, e.Code = c.format.DecodeError(reply)
		return nil, e
	}
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return reply, nil
}
