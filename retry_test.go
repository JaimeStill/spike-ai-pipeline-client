package client_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/format/completions"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/clienttest"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// step is the scripted transport's answer to one attempt: err, or a reply of status with body
// and, when set, a Retry-After header.
type step struct {
	status     int
	body       string
	retryAfter string
	err        error
}

// sent is one request the scripted transport received: its body, its Authorization header, and
// when it arrived, on the bubble's clock.
type sent struct {
	body string
	auth string
	at   time.Time
}

// script is an in-memory transport that answers the nth request with steps[n], repeating the
// last step once they run out, and records each request.
type script struct {
	steps []step
	sent  []sent
}

func (s *script) RoundTrip(r *http.Request) (*http.Response, error) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	s.sent = append(s.sent, sent{body: string(b), auth: r.Header.Get("Authorization"), at: time.Now()})
	st := s.steps[min(len(s.sent), len(s.steps))-1]
	if st.err != nil {
		return nil, st.err
	}
	h := http.Header{}
	if st.retryAfter != "" {
		h.Set("Retry-After", st.retryAfter)
	}
	return &http.Response{StatusCode: st.status, Header: h, Body: io.NopCloser(strings.NewReader(st.body)), Request: r}, nil
}

// retrying returns a Client of policy over a script of steps, its token source numbering each
// call, and its jitter source jitter, or the Client's own when nil.
func retrying(t *testing.T, policy client.RetryPolicy, jitter func(time.Duration) time.Duration, steps ...step) (*client.Client, *script) {
	t.Helper()
	s := &script{steps: steps}
	var tokens int
	c := newClient(t, client.Config{
		BaseURL: "http://router/v1",
		HTTP:    &http.Client{Transport: s},
		Retry:   &policy,
		Token: func(context.Context) (string, error) {
			tokens++
			return fmt.Sprintf("t%d", tokens), nil
		},
	})
	if jitter != nil {
		client.SetJitter(c, jitter)
	}
	return c, s
}

// whole is a jitter source that waits the backoff's whole cap.
func whole(d time.Duration) time.Duration { return d }

// policy is a retry policy of 3 attempts, base 1s and most 10s.
var policy = client.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 10 * time.Second}

// operation is one of the retried operations, run on c, reporting its error.
type operation struct {
	name string
	// ok is the 2xx reply that ends it.
	ok  string
	run func(ctx context.Context, c *client.Client) error
}

var operations = []operation{
	{"Generate", okReply, func(ctx context.Context, c *client.Client) error {
		_, err := c.Generate(ctx, hello())
		return err
	}},
	{"Embed", embedReply, func(ctx context.Context, c *client.Client) error {
		_, err := c.Embed(ctx, two())
		return err
	}},
	{"GenerateStream", eventStream(streamed...), func(ctx context.Context, c *client.Client) error {
		var errs []error
		for _, err := range c.GenerateStream(ctx, hello()) {
			if err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}},
}

func TestNewRefusesRetryPolicy(t *testing.T) {
	cases := []struct {
		name   string
		policy client.RetryPolicy
		want   string
	}{
		{"no attempts", client.RetryPolicy{MaxDelay: time.Second}, "fewer than one attempt"},
		{"negative attempts", client.RetryPolicy{MaxAttempts: -1}, "fewer than one attempt"},
		{"negative base delay", client.RetryPolicy{MaxAttempts: 2, BaseDelay: -1, MaxDelay: time.Second}, "negative delay"},
		{"negative most delay", client.RetryPolicy{MaxAttempts: 2, MaxDelay: -1}, "negative delay"},
		{"base above most", client.RetryPolicy{MaxAttempts: 2, BaseDelay: 2 * time.Second, MaxDelay: time.Second}, "base delay is above"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := client.New(client.Config{BaseURL: "http://h/v1", HTTP: http.DefaultClient, Format: completions.New(), Retry: &tc.policy})
			if err == nil {
				t.Fatalf("New = %v, nil; want an error containing %q", c, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("New error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
	for _, p := range []client.RetryPolicy{
		{MaxAttempts: 1},
		{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Second},
		{MaxAttempts: 3, MaxDelay: time.Minute},
	} {
		if _, err := client.New(client.Config{BaseURL: "http://h/v1", HTTP: http.DefaultClient, Format: completions.New(), Retry: &p}); err != nil {
			t.Errorf("New of policy %+v: %v", p, err)
		}
	}
}

// TestRetryStatuses has each operation answered first by a status the policy retries, and then
// by its 2xx reply, and wants it to succeed on the second attempt, which called the token source
// again and sent the same body.
func TestRetryStatuses(t *testing.T) {
	for _, op := range operations {
		for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			t.Run(fmt.Sprintf("%s/%d", op.name, status), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c, s := retrying(t, policy, nil, step{status: status}, step{status: http.StatusOK, body: op.ok})
					if err := op.run(t.Context(), c); err != nil {
						t.Fatalf("%s: %v", op.name, err)
					}
					if len(s.sent) != 2 {
						t.Fatalf("%d requests sent, want 2", len(s.sent))
					}
					if s.sent[0].body != s.sent[1].body || s.sent[0].body == "" {
						t.Errorf("bodies = %q and %q, want the same body twice", s.sent[0].body, s.sent[1].body)
					}
					if s.sent[0].auth != "Bearer t1" || s.sent[1].auth != "Bearer t2" {
						t.Errorf("Authorization = %q, %q; want a token per attempt, Bearer t1 and Bearer t2", s.sent[0].auth, s.sent[1].auth)
					}
				})
			})
		}
	}
}

// TestRetryNotRetried wants a status the policy doesn't name returned at once, as the *APIError
// of the one request sent.
func TestRetryNotRetried(t *testing.T) {
	for _, op := range operations {
		for _, status := range []int{http.StatusInternalServerError, http.StatusBadRequest, http.StatusBadGateway, http.StatusGatewayTimeout} {
			t.Run(fmt.Sprintf("%s/%d", op.name, status), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c, s := retrying(t, policy, nil, step{status: status}, step{status: http.StatusOK, body: op.ok})
					err := op.run(t.Context(), c)
					if apiErr, ok := errors.AsType[*client.APIError](err); !ok || apiErr.Status != status {
						t.Errorf("%s error = %v, want the *APIError of %d", op.name, err, status)
					}
					if len(s.sent) != 1 {
						t.Errorf("%d requests sent, want 1", len(s.sent))
					}
				})
			})
		}
	}
}

// TestRetryUnsetRetriesNothing wants a Client without a policy to send one request for a 503.
func TestRetryUnsetRetriesNothing(t *testing.T) {
	s := &script{steps: []step{{status: http.StatusServiceUnavailable}, {status: http.StatusOK, body: okReply}}}
	c := newClient(t, client.Config{BaseURL: "http://router/v1", HTTP: &http.Client{Transport: s}})
	_, err := c.Generate(t.Context(), hello())
	if apiErr, ok := errors.AsType[*client.APIError](err); !ok || apiErr.Status != http.StatusServiceUnavailable {
		t.Errorf("Generate error = %v, want the 503's *APIError", err)
	}
	if len(s.sent) != 1 {
		t.Errorf("%d requests sent, want 1", len(s.sent))
	}
}

// TestRetryStartedStream has a stream break mid-body under a policy, and wants its one error
// pair after the events before it, from one request: a started stream is never sent again.
func TestRetryStartedStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, s := retrying(t, policy, nil, step{status: http.StatusOK, body: eventStream(streamed[:3]...)},
			step{status: http.StatusOK, body: eventStream(streamed...)})
		events, resp, errs := collect(t, c.GenerateStream(t.Context(), hello()))
		if len(errs) != 1 || !errors.Is(errs[0], io.ErrUnexpectedEOF) {
			t.Fatalf("GenerateStream errors = %v, want one io.ErrUnexpectedEOF", errs)
		}
		if len(events) == 0 || resp.Status != protocol.StatusInProgress {
			t.Errorf("%d events folding to status %q, want the events before the break, in progress", len(events), resp.Status)
		}
		if len(s.sent) != 1 {
			t.Errorf("%d requests sent, want 1", len(s.sent))
		}
	})
}

// TestRetryConnectionFailure has the first attempt fail in the transport, and wants a connection
// failure before any reply retried, and any other failure returned at once.
func TestRetryConnectionFailure(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	reset := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	cases := []struct {
		name  string
		err   error
		retry bool
	}{
		{"dial refused", refused, true},
		{"connection reset", reset, true},
		{"closed before the reply", fmt.Errorf("transport: %w", io.EOF), true},
		{"closed mid-header", io.ErrUnexpectedEOF, true},
		{"temporary DNS failure", &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Name: "router", IsTemporary: true}}, true},
		{"unknown host", &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Name: "router", IsNotFound: true}}, false},
		{"another failure", errors.New("certificate refused"), false},
		{"context error from the transport", context.Canceled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, s := retrying(t, policy, nil, step{err: tc.err}, step{status: http.StatusOK, body: okReply})
				_, err := c.Generate(t.Context(), hello())
				if tc.retry {
					if err != nil {
						t.Errorf("Generate: %v", err)
					}
					if len(s.sent) != 2 {
						t.Errorf("%d requests sent, want 2", len(s.sent))
					}
					return
				}
				if !errors.Is(err, tc.err) {
					t.Errorf("Generate error = %v, want it to wrap %v", err, tc.err)
				}
				if len(s.sent) != 1 {
					t.Errorf("%d requests sent, want 1", len(s.sent))
				}
			})
		})
	}
}

// TestRetryConnectionClosed has a real server close the first connection before replying, and
// wants the request sent again and answered.
func TestRetryConnectionClosed(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okReply)
	}))
	t.Cleanup(srv.Close)
	p := client.RetryPolicy{MaxAttempts: 2}
	c := newClient(t, client.Config{BaseURL: srv.URL, Retry: &p})
	if _, err := c.Generate(t.Context(), hello()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("server received %d requests, want 2", n)
	}
}

// TestRetryAfter wants a Retry-After, in seconds or as an HTTP date, to replace the backoff, and
// one that is negative or unreadable ignored, leaving the backoff, here its whole cap of 1s.
func TestRetryAfter(t *testing.T) {
	cases := []struct {
		name string
		// retryAfter is the header, its date's offset from now formatted in.
		retryAfter func(now time.Time) string
		wait       time.Duration
	}{
		{"seconds", func(time.Time) string { return "3" }, 3 * time.Second},
		{"zero seconds", func(time.Time) string { return "0" }, 0},
		{"HTTP date", func(now time.Time) string { return now.Add(5 * time.Second).UTC().Format(http.TimeFormat) }, 5 * time.Second},
		{"HTTP date passed", func(now time.Time) string { return now.Add(-time.Minute).UTC().Format(http.TimeFormat) }, 0},
		{"negative", func(time.Time) string { return "-3" }, time.Second},
		{"unreadable", func(time.Time) string { return "soon" }, time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, s := retrying(t, policy, whole,
					step{status: http.StatusTooManyRequests, retryAfter: tc.retryAfter(time.Now())},
					step{status: http.StatusOK, body: okReply})
				if _, err := c.Generate(t.Context(), hello()); err != nil {
					t.Fatalf("Generate: %v", err)
				}
				if len(s.sent) != 2 {
					t.Fatalf("%d requests sent, want 2", len(s.sent))
				}
				if got := s.sent[1].at.Sub(s.sent[0].at); got != tc.wait {
					t.Errorf("waited %v, want %v", got, tc.wait)
				}
			})
		})
	}
}

// TestRetryReturnsAtOnce wants the last error returned without a wait when the wait is longer
// than the policy's most delay, or would end past the context's deadline.
func TestRetryReturnsAtOnce(t *testing.T) {
	cases := []struct {
		name       string
		retryAfter string
		timeout    time.Duration
	}{
		{"Retry-After over the most delay", "11", 0},
		{"Retry-After too long to hold", "99999999999999999999", 0},
		{"Retry-After past the deadline", "5", 4 * time.Second},
		{"backoff past the deadline", "", 500 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()
				if tc.timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tc.timeout)
					defer cancel()
				}
				c, s := retrying(t, policy, whole,
					step{status: http.StatusServiceUnavailable, body: "busy", retryAfter: tc.retryAfter},
					step{status: http.StatusOK, body: okReply})
				start := time.Now()
				_, err := c.Generate(ctx, hello())
				if apiErr, ok := errors.AsType[*client.APIError](err); !ok || apiErr.Status != http.StatusServiceUnavailable || string(apiErr.Body) != "busy" {
					t.Errorf("Generate error = %v, want the 503's *APIError", err)
				}
				if waited := time.Since(start); waited != 0 {
					t.Errorf("waited %v, want none", waited)
				}
				if len(s.sent) != 1 {
					t.Errorf("%d requests sent, want 1", len(s.sent))
				}
			})
		})
	}
}

// TestRetryCancelWhileWaiting cancels the context during the wait, and wants the context's
// error, not the attempt's.
func TestRetryCancelWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		c, s := retrying(t, policy, nil, step{status: http.StatusServiceUnavailable, retryAfter: "10"},
			step{status: http.StatusOK, body: okReply})
		go func() {
			time.Sleep(time.Second)
			cancel()
		}()
		start := time.Now()
		_, err := c.Generate(ctx, hello())
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Generate error = %v, want context.Canceled", err)
		}
		if _, ok := errors.AsType[*client.APIError](err); ok {
			t.Errorf("Generate error = %v, an *APIError; want the context's", err)
		}
		if waited := time.Since(start); waited != time.Second {
			t.Errorf("waited %v, want 1s, until the cancel", waited)
		}
		if len(s.sent) != 1 {
			t.Errorf("%d requests sent, want 1", len(s.sent))
		}
	})
}

// TestRetryExhausted has every attempt answered 503, and wants MaxAttempts requests and the last
// attempt's *APIError.
func TestRetryExhausted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, s := retrying(t, policy, nil,
			step{status: http.StatusServiceUnavailable, body: "attempt 1"},
			step{status: http.StatusTooManyRequests, body: "attempt 2"},
			step{status: http.StatusServiceUnavailable, body: "attempt 3"},
			step{status: http.StatusOK, body: okReply})
		_, err := c.Generate(t.Context(), hello())
		apiErr, ok := errors.AsType[*client.APIError](err)
		if !ok || apiErr.Status != http.StatusServiceUnavailable || string(apiErr.Body) != "attempt 3" {
			t.Errorf("Generate error = %v, want the third attempt's *APIError", err)
		}
		if len(s.sent) != 3 {
			t.Errorf("%d requests sent, want 3", len(s.sent))
		}
	})
}

// TestRetryBackoff has every attempt answered 503, with a jitter source that records the cap it
// is given and waits half of it, and wants the caps to double from the base delay to the most
// delay, and each wait the jitter's.
func TestRetryBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var caps []time.Duration
		half := func(d time.Duration) time.Duration {
			caps = append(caps, d)
			return d / 2
		}
		p := client.RetryPolicy{MaxAttempts: 6, BaseDelay: time.Second, MaxDelay: 5 * time.Second}
		c, s := retrying(t, p, half, step{status: http.StatusServiceUnavailable})
		if _, err := c.Generate(t.Context(), hello()); err == nil {
			t.Fatal("Generate succeeded, want the 503's *APIError")
		}
		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
		if !slices.Equal(caps, want) {
			t.Errorf("backoff caps = %v, want %v", caps, want)
		}
		if len(s.sent) != len(want)+1 {
			t.Fatalf("%d requests sent, want %d", len(s.sent), len(want)+1)
		}
		for i, d := range want {
			if got := s.sent[i+1].at.Sub(s.sent[i].at); got != d/2 {
				t.Errorf("wait %d = %v, want %v", i+1, got, d/2)
			}
		}
	})
}

// TestFullJitter wants the Client's own jitter source to wait from 0 up to the cap.
func TestFullJitter(t *testing.T) {
	if got := client.FullJitter(0); got != 0 {
		t.Errorf("FullJitter(0) = %v, want 0", got)
	}
	const d = 4 * time.Nanosecond
	seen := map[time.Duration]bool{}
	for range 1000 {
		w := client.FullJitter(d)
		if w < 0 || w > d {
			t.Fatalf("FullJitter(%v) = %v, want 0 to %v", d, w, d)
		}
		seen[w] = true
	}
	if len(seen) != 5 {
		t.Errorf("FullJitter(%v) gave %d distinct waits in 1000, want all 5 from 0 to %v", d, len(seen), d)
	}
	if w := client.FullJitter(time.Duration(math.MaxInt64)); w < 0 {
		t.Errorf("FullJitter of the longest duration = %v, want at least 0", w)
	}
}

// TestTranscribeNotRetried has a policy and a 503, and wants one request and its *APIError: the
// clip is read as it is sent, so Transcribe is never sent again.
func TestTranscribeNotRetried(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusServiceUnavailable, `{"error":{"message":"loading"}}`)
	p := client.RetryPolicy{MaxAttempts: 3}
	c := newClient(t, client.Config{BaseURL: srv.URL, Retry: &p})
	_, err := c.Transcribe(t.Context(), &client.TranscriptionRequest{
		Model: "m", File: strings.NewReader("RIFF"), FileName: "clip.wav",
	})
	if apiErr, ok := errors.AsType[*client.APIError](err); !ok || apiErr.Status != http.StatusServiceUnavailable {
		t.Errorf("Transcribe error = %v, want the 503's *APIError", err)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("server received %d requests, want 1", n)
	}
}
