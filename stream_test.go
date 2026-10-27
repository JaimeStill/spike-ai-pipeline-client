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
	"syscall"
	"testing"
	"testing/synctest"
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

// errorFrame is the event llama.cpp sends when generation fails mid-stream, ahead of ending the
// stream: format_oai_sse of {"error": format_error_response(...)}.
const errorFrame = `{"error":{"code":500,"message":"context shift is disabled","type":"server_error"}}`

// partial is the Response a stream of streamed's chunks folds to when it breaks after its
// message's text: in progress, holding text.
func partial(text string) *protocol.Response {
	return &protocol.Response{Status: protocol.StatusInProgress, Output: []protocol.Item{protocol.Message{
		Status:  protocol.StatusInProgress,
		Role:    protocol.RoleAssistant,
		Content: []protocol.ContentPart{protocol.OutputText{Text: text}},
	}}}
}

// body is a reply body that reads its text and then fails with err, or, when hang is set, waits
// for ctx and then fails as the transport does once a request's context is done. It records
// whether it was closed.
type body struct {
	text   io.Reader
	err    error
	hang   bool
	ctx    context.Context
	closed atomic.Bool
}

func (b *body) Read(p []byte) (int, error) {
	n, err := b.text.Read(p)
	if !errors.Is(err, io.EOF) {
		return n, err
	}
	switch {
	case b.hang:
		<-b.ctx.Done()
		return 0, errors.New("net/http: request canceled")
	case b.err != nil:
		return 0, b.err
	}
	return 0, io.EOF
}

func (b *body) Close() error {
	b.closed.Store(true)
	return nil
}

// roundTrip is an http.RoundTripper of a function.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestGenerateStreamError covers each error that breaks a stream once it has begun, in a bubble
// whose transport is in memory, so a goroutine left behind would fail the test: the error ends
// the sequence as its last pair, after the events before it, which fold to the partial Response
// in progress, and the body is closed.
func TestGenerateStreamError(t *testing.T) {
	broken := eventStream(streamed[:3]...)
	cases := []struct {
		name string
		// reply is the body, read before err; hang waits after it for the context, which
		// timeout ends; cancel cancels the context at the first output_text delta.
		reply   string
		err     error
		hang    bool
		timeout time.Duration
		cancel  bool
		// text is the partial Response's text, and check the error's.
		text  string
		check func(t *testing.T, err error)
	}{
		{name: "stream ended before its terminal event", reply: broken, text: "Paris", check: func(t *testing.T, err error) {
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("error = %v, want io.ErrUnexpectedEOF", err)
			}
		}},
		{name: "connection dropped", reply: broken, err: io.ErrUnexpectedEOF, text: "Paris", check: func(t *testing.T, err error) {
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("error = %v, want io.ErrUnexpectedEOF", err)
			}
		}},
		{name: "connection reset", reply: broken, err: syscall.ECONNRESET, text: "Paris", check: func(t *testing.T, err error) {
			if !errors.Is(err, io.ErrUnexpectedEOF) || !errors.Is(err, syscall.ECONNRESET) {
				t.Errorf("error = %v, want io.ErrUnexpectedEOF wrapping the reset", err)
			}
		}},
		{name: "chunk the format can't decode", reply: eventStream(append(streamed[:3:3], `{"choices":`)...), text: "Paris", check: func(t *testing.T, err error) {
			if !strings.Contains(err.Error(), "decode chunk") || errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("error = %v, want the format's decode error", err)
			}
		}},
		{name: "context cancelled", reply: eventStream(streamed...), cancel: true, text: "Par", check: func(t *testing.T, err error) {
			if !errors.Is(err, context.Canceled) {
				t.Errorf("error = %v, want context.Canceled", err)
			}
		}},
		{name: "context deadline", reply: broken, hang: true, timeout: time.Minute, text: "Paris", check: func(t *testing.T, err error) {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("error = %v, want context.DeadlineExceeded", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if tc.timeout > 0 {
					ctx, cancel = context.WithTimeout(ctx, tc.timeout)
					defer cancel()
				}
				var b *body
				c := newClient(t, client.Config{BaseURL: "http://router/v1", HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
					b = &body{text: strings.NewReader(tc.reply), err: tc.err, hang: tc.hang, ctx: r.Context()}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: b, Request: r}, nil
				})}})
				var acc protocol.Accumulator
				var errs []error
				for e, err := range c.GenerateStream(ctx, hello()) {
					if err != nil {
						errs = append(errs, err)
						continue
					}
					if len(errs) > 0 || ctx.Err() != nil {
						t.Errorf("event %#v follows the error or the cancel", e)
					}
					if err := acc.Add(e); err != nil {
						t.Errorf("Accumulator.Add: %v", err)
					}
					if _, ok := e.(protocol.OutputTextDelta); ok && tc.cancel {
						cancel()
					}
				}
				if len(errs) != 1 {
					t.Fatalf("GenerateStream errors = %v, want one", errs)
				}
				tc.check(t, errs[0])
				if got, want := acc.Response(), partial(tc.text); !reflect.DeepEqual(got, want) {
					t.Errorf("Response = %#v\nwant       %#v", got, want)
				}
				if !b.closed.Load() {
					t.Error("the body is open after the sequence ended")
				}
			})
		})
	}
}

// TestGenerateStreamCancel cancels the context mid-stream, against a server that keeps
// generating until the client goes, and wants the context's error, the partial Response holding
// the text so far, and the connection closed, which stops the generation.
func TestGenerateStreamCancel(t *testing.T) {
	gone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(gone)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", streamed[0])
		for {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", streamed[1])
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
	}))
	t.Cleanup(srv.Close)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var acc protocol.Accumulator
	var text strings.Builder
	var errs []error
	for e, err := range c.GenerateStream(ctx, hello()) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ctx.Err() != nil {
			t.Errorf("event %#v follows the cancel", e)
		}
		if err := acc.Add(e); err != nil {
			t.Errorf("Accumulator.Add: %v", err)
		}
		if d, ok := e.(protocol.OutputTextDelta); ok {
			if text.WriteString(d.Delta); text.Len() >= 3*len("Par") {
				cancel()
			}
		}
	}
	if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Fatalf("GenerateStream errors = %v, want one context.Canceled", errs)
	}
	if got, want := acc.Response(), partial(text.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("Response = %#v\nwant       %#v", got, want)
	}
	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("the server is still generating after the cancel")
	}
}

// TestGenerateStreamConnectionDropped has the server drop the connection mid-stream, and wants
// io.ErrUnexpectedEOF after the events it sent, which fold to the partial Response.
func TestGenerateStreamConnectionDropped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, eventStream(streamed[:3]...))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	_, resp, errs := collect(t, c.GenerateStream(t.Context(), hello()))
	if len(errs) != 1 || !errors.Is(errs[0], io.ErrUnexpectedEOF) {
		t.Fatalf("GenerateStream errors = %v, want one io.ErrUnexpectedEOF", errs)
	}
	if want := partial("Paris"); !reflect.DeepEqual(resp, want) {
		t.Errorf("Response = %#v\nwant       %#v", resp, want)
	}
}

// TestGenerateStreamErrorFrame has the server send llama.cpp's error frame, mid-stream and as the
// stream's first event, and wants no error pair: the stream ends as the spec has it, with the
// error event of the frame's type, code, and message, then response.failed, which folds to the
// Response failed, holding the frame's error and the output made before it, the message open at
// it in progress. Nothing of the chunk the server sends after the frame is yielded.
func TestGenerateStreamErrorFrame(t *testing.T) {
	payload := protocol.ErrorPayload{Type: "server_error", Code: "500", Message: "context shift is disabled"}
	cases := []struct {
		name   string
		chunks []string
		// output is the failed Response's.
		output []protocol.Item
	}{
		{"mid-stream", append(streamed[:3:3], errorFrame, streamed[2]), partial("Paris").Output},
		{"first", []string{errorFrame, streamed[1]}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, http.StatusOK, eventStream(tc.chunks...))
			c := newClient(t, client.Config{BaseURL: srv.URL})
			events, resp, errs := collect(t, c.GenerateStream(t.Context(), hello()))
			if len(errs) > 0 {
				t.Fatalf("GenerateStream errors = %v, want none", errs)
			}
			if _, ok := events[0].(protocol.ResponseCreated); !ok {
				t.Errorf("first event = %#v, want response.created", events[0])
			}
			n := len(events)
			if e, ok := events[n-2].(protocol.ErrorEvent); !ok || e.Error != payload {
				t.Errorf("next to last event = %#v, want the error event of %#v", events[n-2], payload)
			}
			if _, ok := events[n-1].(protocol.ResponseFailed); !ok {
				t.Errorf("last event = %#v, want response.failed", events[n-1])
			}
			want := &protocol.Response{
				Output: tc.output,
				Status: protocol.StatusFailed,
				Error:  &protocol.ResponseError{Code: "500", Message: "context shift is disabled"},
			}
			if !reflect.DeepEqual(resp, want) {
				t.Errorf("Response = %#v\nwant       %#v", resp, want)
			}
		})
	}
}

// TestGenerateStreamIgnoresPings interleaves llama.cpp's ping comments, ":" lines, with the
// stream's events, and wants the same events and the same Response as the stream without them.
func TestGenerateStreamIgnoresPings(t *testing.T) {
	var b strings.Builder
	for _, c := range streamed {
		fmt.Fprintf(&b, ":\n\ndata: %s\n\n: ping\n", c)
	}
	pinged := clienttest.NewServer(t, http.StatusOK, b.String())
	plain := clienttest.NewServer(t, http.StatusOK, eventStream(streamed...))
	events, got, errs := collect(t, newClient(t, client.Config{BaseURL: pinged.URL}).GenerateStream(t.Context(), hello()))
	if len(errs) > 0 {
		t.Fatalf("GenerateStream errors: %v", errs)
	}
	wantEvents, want, _ := collect(t, newClient(t, client.Config{BaseURL: plain.URL}).GenerateStream(t.Context(), hello()))
	if !reflect.DeepEqual(events, wantEvents) || !reflect.DeepEqual(got, want) {
		t.Errorf("with pings: %d events, Response %#v\nwant %d events, Response %#v", len(events), got, len(wantEvents), want)
	}
}
