package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryPolicy is when and how long the Client waits to send a request again. It is the caller's
// policy, set in Config.Retry; unset, nothing is retried.
//
// A request is sent again only when its attempt ends without a 2xx reply in a way that says the
// endpoint may answer later: a 408 Request Timeout, 429 Too Many Requests, or 503 Service
// Unavailable reply, or a connection failure before any reply, such as a refused dial or a
// connection reset or closed before the reply's header. Nothing else is: not a 500 or any other
// status, not a cancelled or expired context, not a failure of the token source, and never a
// stream once its 2xx reply has arrived.
//
// A connection that fails before the reply may fail after the endpoint has read the request,
// and the requests the Client sends are POSTs, which nothing marks as safe to repeat: a retried
// generation may run twice on the endpoint, and be counted twice. The caller who sets a policy
// accepts that.
//
// Each attempt calls the token source again and sends the same encoded body. Between attempts
// the Client waits an exponential backoff with full jitter: before attempt n+1, a random wait of
// up to BaseDelay doubled n-1 times, capped at MaxDelay. A reply's Retry-After, in seconds or as
// an HTTP date, replaces the backoff; one that can't be read, or is negative, is ignored. When
// the wait is longer than MaxDelay, which only a Retry-After makes it, or would end past ctx's
// deadline, the Client doesn't wait: it returns the last attempt's error at once. A ctx cancelled
// while waiting returns ctx's error. Once MaxAttempts are made, the last attempt's error is
// returned, as it is without a policy, such as the *APIError of the last reply.
type RetryPolicy struct {
	// MaxAttempts is the most requests sent for one call, the first included, at least 1. 1 sends
	// one and retries nothing.
	MaxAttempts int
	// BaseDelay is the cap on the wait before the second attempt, which doubles for each attempt
	// after it. It is at least 0 and at most MaxDelay.
	BaseDelay time.Duration
	// MaxDelay is the cap on any wait, at least 0, and the longest Retry-After the Client
	// honors.
	MaxDelay time.Duration
}

// check reports a policy New refuses: fewer than one attempt, a negative delay, or a base delay
// above the most delay.
func (p RetryPolicy) check() error {
	switch {
	case p.MaxAttempts < 1:
		return errors.New("client: retry policy has fewer than one attempt")
	case p.BaseDelay < 0 || p.MaxDelay < 0:
		return errors.New("client: retry policy has a negative delay")
	case p.BaseDelay > p.MaxDelay:
		return errors.New("client: retry policy's base delay is above its most delay")
	}
	return nil
}

// backoff is the cap on the wait before attempt n+1, after n attempts: BaseDelay doubled n-1
// times, at most MaxDelay. The doubling stops at MaxDelay, so it can't overflow.
func (p RetryPolicy) backoff(n int) time.Duration {
	d := p.BaseDelay
	for i := 1; i < n && d < p.MaxDelay; i++ {
		if d > p.MaxDelay/2 {
			return p.MaxDelay
		}
		d *= 2
	}
	return min(d, p.MaxDelay)
}

// fullJitter is the Client's jitter source: a random duration from 0 up to d, d included.
func fullJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	if d == time.Duration(math.MaxInt64) {
		return rand.N(d)
	}
	return rand.N(d + 1)
}

// send sends body as a POST of contentType to route, accepting accept, with header, and returns
// a 2xx reply, its body unread, for the caller to read and close. Each attempt is attempt's,
// sending the same body; with the Client's retry policy, an attempt that fails as RetryPolicy
// describes is made again after the wait it describes. Every operation whose body is held whole
// sends through here, so this is the one retry loop: Generate, Embed, and GenerateStream, whose
// stream starts only once send returns.
func (c *Client) send(ctx context.Context, route string, header http.Header, body []byte, contentType, accept string,
	decodeError func(body []byte) (message, typ, code string)) (*http.Response, error) {
	attempts := max(c.retry.MaxAttempts, 1)
	for n := 1; ; n++ {
		resp, retryAfter, err := c.attempt(ctx, route, header, bytes.NewReader(body), contentType, accept, decodeError)
		if err == nil || n == attempts || !retryable(ctx, err) {
			return resp, err
		}
		wait, ok := parseRetryAfter(retryAfter, time.Now())
		if !ok {
			wait = c.jitter(c.retry.backoff(n))
		}
		if wait > c.retry.MaxDelay {
			return nil, err
		}
		if deadline, ok := ctx.Deadline(); ok && time.Now().Add(wait).After(deadline) {
			return nil, err
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// retryable reports whether an attempt that failed with err may be made again: a 408, 429, or
// 503 reply, or a connection failure before any reply while ctx is live.
func retryable(ctx context.Context, err error) bool {
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		switch apiErr.Status {
		case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusServiceUnavailable:
			return true
		}
		return false
	}
	// A transport error once ctx is done is ctx's doing, not the connection's.
	if ctx.Err() != nil {
		return false
	}
	return connectionFailure(err)
}

// connectionFailure reports whether err, from sending a request, is the connection failing
// before any reply: a network operation's error, such as a refused dial or a reset, or the
// connection closed before the reply's header, which the transport reports as io.EOF or
// io.ErrUnexpectedEOF. A host name that doesn't resolve isn't, unless the resolver calls its
// failure temporary. Anything else, such as the transport's ResponseHeaderTimeout, a TLS
// certificate the transport refuses, or an error of the caller's own RoundTripper, isn't: the
// list is short so that only what a later attempt can mend is retried.
func connectionFailure(err error) bool {
	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		return dnsErr.IsTemporary
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// parseRetryAfter reads a Retry-After header's value, h, at now: whole seconds, or an HTTP date,
// which http.ParseTime reads, its wait the time from now until it, or none for a date passed. It
// reports false for an empty, negative, or unreadable value, which leaves the wait to the
// backoff. Seconds too many to hold are the longest wait there is, which exceeds any policy's
// MaxDelay.
func parseRetryAfter(h string, now time.Time) (time.Duration, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, false
	}
	if strings.Trim(h, "0123456789") == "" {
		secs, err := strconv.ParseInt(h, 10, 64)
		if err != nil || secs > int64(time.Duration(math.MaxInt64)/time.Second) {
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(secs) * time.Second, true
	}
	t, err := http.ParseTime(h)
	if err != nil {
		return 0, false
	}
	return max(t.Sub(now), 0), true
}
