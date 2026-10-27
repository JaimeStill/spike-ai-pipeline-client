package client_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/clienttest"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// streamed is a streamed Chat Completions reply of "Paris", as the router writes it: data
// events, a usage chunk, and [DONE].
var streamed = []string{
	`{"choices":[{"index":0,"delta":{"role":"assistant","content":null},"finish_reason":null}]}`,
	`{"choices":[{"index":0,"delta":{"content":"Par"},"finish_reason":null}]}`,
	`{"choices":[{"index":0,"delta":{"content":"is"},"finish_reason":null}]}`,
	`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	`{"choices":[],"usage":{"completion_tokens":2,"prompt_tokens":9,"total_tokens":11}}`,
	`[DONE]`,
}

// eventStream is a text/event-stream body of chunks, one data event each. The test server
// labels it application/json, which the client doesn't read.
func eventStream(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		fmt.Fprintf(&b, "data: %s\n\n", c)
	}
	return b.String()
}

// collect ranges seq to its end, folding its events with an Accumulator, and returns the
// events, the Response folded, and the errors it yielded.
func collect(t *testing.T, seq func(func(protocol.Event, error) bool)) ([]protocol.Event, *protocol.Response, []error) {
	t.Helper()
	var acc protocol.Accumulator
	var events []protocol.Event
	var errs []error
	for e, err := range seq {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(errs) > 0 {
			t.Errorf("event %#v follows an error", e)
		}
		events = append(events, e)
		if err := acc.Add(e); err != nil {
			t.Errorf("Accumulator.Add: %v", err)
		}
	}
	return events, acc.Response(), errs
}

func TestGenerateStream(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, eventStream(streamed...))
	c := newClient(t, client.Config{BaseURL: srv.URL + "/v1"})
	events, resp, errs := collect(t, c.GenerateStream(t.Context(), hello()))
	if len(errs) > 0 {
		t.Fatalf("GenerateStream errors: %v", errs)
	}
	if _, ok := events[len(events)-1].(protocol.ResponseCompleted); !ok {
		t.Errorf("last event = %#v, want response.completed", events[len(events)-1])
	}
	want := &protocol.Response{
		Output: []protocol.Item{protocol.Message{
			Status:  protocol.StatusCompleted,
			Role:    protocol.RoleAssistant,
			Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}},
		}},
		Status: protocol.StatusCompleted,
		Usage:  protocol.Usage{InputTokens: 9, OutputTokens: 2, TotalTokens: 11},
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("Response = %#v\nwant       %#v", resp, want)
	}
	r := srv.Only(t)
	if r.Path != "/v1/chat/completions" {
		t.Errorf("path = %s, want /v1/chat/completions", r.Path)
	}
	if got := r.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
	if !strings.Contains(string(r.Body), `"stream":true`) {
		t.Errorf("body = %s, want it streamed", r.Body)
	}
}

// TestGenerateStreamIgnoresEventNames wants a stream whose events are named folded as the same
// stream unnamed: the client hands the format each event's data alone.
func TestGenerateStreamIgnoresEventNames(t *testing.T) {
	var b strings.Builder
	for _, c := range streamed {
		fmt.Fprintf(&b, "event: chunk\ndata: %s\n\n", c)
	}
	named := clienttest.NewServer(t, http.StatusOK, b.String())
	unnamed := clienttest.NewServer(t, http.StatusOK, eventStream(streamed...))
	_, got, errs := collect(t, newClient(t, client.Config{BaseURL: named.URL}).GenerateStream(t.Context(), hello()))
	if len(errs) > 0 {
		t.Fatalf("GenerateStream errors: %v", errs)
	}
	_, want, _ := collect(t, newClient(t, client.Config{BaseURL: unnamed.URL}).GenerateStream(t.Context(), hello()))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Response = %#v\nwant       %#v", got, want)
	}
}

// TestGenerateStreamIsLazy wants nothing validated, encoded, or sent until the sequence is
// ranged, and a new request each time it is.
func TestGenerateStreamIsLazy(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, eventStream(streamed...))
	var tokens atomic.Int32
	c := newClient(t, client.Config{
		BaseURL: srv.URL,
		Token:   func(context.Context) (string, error) { tokens.Add(1); return "", nil },
	})
	seq := c.GenerateStream(t.Context(), hello())
	if n := len(srv.Requests()); n != 0 || tokens.Load() != 0 {
		t.Fatalf("before ranging: %d requests and %d tokens, want none", n, tokens.Load())
	}
	for range 2 {
		if _, _, errs := collect(t, seq); len(errs) > 0 {
			t.Fatalf("GenerateStream errors: %v", errs)
		}
	}
	if n := len(srv.Requests()); n != 2 || tokens.Load() != 2 {
		t.Errorf("after ranging twice: %d requests and %d tokens, want 2 of each", n, tokens.Load())
	}
}

// TestGenerateStreamRefusal covers the failures before the stream: each is the sequence's only
// pair, and the refused requests send nothing.
func TestGenerateStreamRefusal(t *testing.T) {
	noModel := hello()
	noModel.Model = ""
	clash := hello()
	clash.Native = []byte(`{"stream":false}`)
	tokenErr := errors.New("token expired")
	cases := []struct {
		name   string
		req    *protocol.Request
		token  client.TokenSource
		status int
		want   string
		sent   int
	}{
		{"invalid request", noModel, nil, http.StatusOK, "no model", 0},
		{"native clash, refused at encode", clash, nil, http.StatusOK, "native member stream clashes", 0},
		{"token source error", hello(), func(context.Context) (string, error) { return "", tokenErr }, http.StatusOK, "token expired", 0},
		{"non-2xx reply", hello(), nil, http.StatusBadRequest, "400 Bad Request: model 'm' not found", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, tc.status, `{"error":{"code":400,"message":"model 'm' not found","type":"invalid_request_error"}}`)
			c := newClient(t, client.Config{BaseURL: srv.URL, Token: tc.token})
			var pairs int
			var err error
			for e, e2 := range c.GenerateStream(t.Context(), tc.req) {
				pairs++
				if e != nil {
					t.Errorf("event %#v, want none", e)
				}
				err = e2
			}
			if pairs != 1 || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("GenerateStream = %d pairs, error %v; want 1 pair with an error containing %q", pairs, err, tc.want)
			}
			if n := len(srv.Requests()); n != tc.sent {
				t.Errorf("server received %d requests, want %d", n, tc.sent)
			}
			if tc.status != http.StatusOK {
				var apiErr *client.APIError
				if !errors.As(err, &apiErr) || apiErr.Status != tc.status || apiErr.Type != "invalid_request_error" {
					t.Errorf("error = %#v, want an *APIError of %d", err, tc.status)
				}
			}
			if tc.token != nil && !errors.Is(err, tokenErr) {
				t.Errorf("error = %v, want it to wrap the token source's", err)
			}
		})
	}
}

// TestGenerateStreamBreakClosesBody breaks out of the loop at the first event of a stream the
// server holds open, and wants the connection closed.
func TestGenerateStreamBreakClosesBody(t *testing.T) {
	closed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", streamed[1])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	t.Cleanup(srv.Close)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	for e, err := range c.GenerateStream(t.Context(), hello()) {
		if err != nil {
			t.Fatalf("GenerateStream error: %v", err)
		}
		if _, ok := e.(protocol.ResponseCreated); !ok {
			t.Fatalf("first event = %#v, want response.created", e)
		}
		break
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection is still open after the loop broke")
	}
}

// TestGenerateStreamEndsEarly wants a stream that ends before its terminal event to end the
// sequence with io.ErrUnexpectedEOF, after the events it made, which fold to the partial
// response.
func TestGenerateStreamEndsEarly(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, eventStream(streamed[:3]...))
	c := newClient(t, client.Config{BaseURL: srv.URL})
	events, resp, errs := collect(t, c.GenerateStream(t.Context(), hello()))
	if len(errs) != 1 || !errors.Is(errs[0], io.ErrUnexpectedEOF) {
		t.Fatalf("GenerateStream errors = %v, want one io.ErrUnexpectedEOF", errs)
	}
	if len(events) == 0 {
		t.Fatal("GenerateStream made no events, want those before the end")
	}
	want := &protocol.Response{Status: protocol.StatusInProgress, Output: []protocol.Item{protocol.Message{
		Status:  protocol.StatusInProgress,
		Role:    protocol.RoleAssistant,
		Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}},
	}}}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("Response = %#v\nwant       %#v", resp, want)
	}
}
