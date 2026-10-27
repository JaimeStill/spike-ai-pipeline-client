// Package clienttest holds the test helpers the module's test packages share: a loopback HTTP
// server that answers with a fixed reply and records the requests it receives.
package clienttest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
)

// Request is one request a Server received, as it arrived on the wire.
type Request struct {
	// Method is the request's method.
	Method string
	// Path is the URL's path.
	Path string
	// RawQuery is the URL's encoded query.
	RawQuery string
	// Header is the request's header.
	Header http.Header
	// Body is the request's body.
	Body []byte
}

// Server is a loopback HTTP server that answers every request with one status and body, and
// records each request.
type Server struct {
	// URL is the server's base URL, such as "http://127.0.0.1:41234".
	URL string

	mu   sync.Mutex
	reqs []Request
}

// NewServer starts a Server that answers with status and reply as application/json. It is
// closed when t's test ends.
func NewServer(t testing.TB, status int, reply string) *Server {
	t.Helper()
	s := &Server{}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("clienttest: read request body: %v", err)
		}
		s.mu.Lock()
		s.reqs = append(s.reqs, Request{
			Method:   r.Method,
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
			Header:   r.Header.Clone(),
			Body:     body,
		})
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(hs.Close)
	s.URL = hs.URL
	return s
}

// Requests returns the requests the server has received, in order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reqs)
}

// Only returns the one request the server received, failing t unless it received exactly one.
func (s *Server) Only(t testing.TB) Request {
	t.Helper()
	reqs := s.Requests()
	if len(reqs) != 1 {
		t.Fatalf("server received %d requests, want 1", len(reqs))
	}
	return reqs[0]
}
