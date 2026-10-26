package client_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/clienttest"
)

// transcribeReply is llama.cpp b11669's transcription reply, as gb10-2 sends it.
const transcribeReply = `{"type":"transcript.text.done","text":"The access code is 7429.",` +
	`"usage":{"type":"tokens","input_tokens":174,"output_tokens":11,"total_tokens":185,` +
	`"input_tokens_details":{"cached_tokens":169}}}`

// waitLimit bounds a wait on something that should happen at once, so a hang fails the test
// rather than stalling it. Nothing sleeps for it.
const waitLimit = 10 * time.Second

// clip returns n bytes of a clip, varied so a reordered or dropped chunk shows.
func clip(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

// formPart is one part of a multipart form as the server read it.
type formPart struct {
	fileName    string
	contentType string
	body        []byte
}

// readForm parses a recorded request's multipart form into its parts by form name, failing t
// unless the request is multipart/form-data with a boundary, each name appearing once.
func readForm(t *testing.T, got clienttest.Request) map[string]formPart {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(got.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		t.Fatalf("Content-Type = %q (%v), want multipart/form-data with a boundary", got.Header.Get("Content-Type"), err)
	}
	parts := make(map[string]formPart)
	r := multipart.NewReader(bytes.NewReader(got.Body), params["boundary"])
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			return parts
		}
		if err != nil {
			t.Fatalf("read form: %v", err)
		}
		body, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("read part %q: %v", p.FormName(), err)
		}
		if _, dup := parts[p.FormName()]; dup {
			t.Fatalf("form has part %q twice", p.FormName())
		}
		parts[p.FormName()] = formPart{fileName: p.FileName(), contentType: p.Header.Get("Content-Type"), body: body}
	}
}

func TestTranscribeWireRequest(t *testing.T) {
	audio := clip(64 << 10)
	cases := []struct {
		name   string
		req    client.TranscriptionRequest
		fields map[string]string
	}{
		{
			"required fields",
			client.TranscriptionRequest{Model: "m", File: bytes.NewReader(audio), FileName: "clip.wav"},
			map[string]string{"model": "m", "response_format": "json"},
		},
		{
			"language and prompt",
			client.TranscriptionRequest{Model: "m", File: bytes.NewReader(audio), FileName: "clip.wav",
				Language: "en", Prompt: "Transcribe the code as digits."},
			map[string]string{"model": "m", "response_format": "json", "language": "en",
				"prompt": "Transcribe the code as digits."},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, http.StatusOK, transcribeReply)
			c := newClient(t, client.Config{
				BaseURL: srv.URL + "/v1/",
				Query:   url.Values{"autoload": {"false"}},
				Token:   func(context.Context) (string, error) { return "secret", nil },
			})
			if _, err := c.Transcribe(t.Context(), tc.req); err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			got := srv.Only(t)
			if got.Method != http.MethodPost || got.Path != "/v1/audio/transcriptions" || got.RawQuery != "autoload=false" {
				t.Errorf("request = %s %s?%s, want POST /v1/audio/transcriptions?autoload=false",
					got.Method, got.Path, got.RawQuery)
			}
			for k, want := range map[string]string{"Accept": "application/json", "Authorization": "Bearer secret"} {
				if v := got.Header.Get(k); v != want {
					t.Errorf("%s = %q, want %q", k, v, want)
				}
			}
			parts := readForm(t, got)
			file, ok := parts["file"]
			if !ok {
				t.Fatalf("form has no file part; parts %v", slices.Sorted(maps.Keys(parts)))
			}
			if file.fileName != "clip.wav" || file.contentType != "application/octet-stream" {
				t.Errorf("file part = name %q, type %q; want clip.wav, application/octet-stream",
					file.fileName, file.contentType)
			}
			if !bytes.Equal(file.body, audio) {
				t.Errorf("file part has %d bytes, not the clip's %d", len(file.body), len(audio))
			}
			delete(parts, "file")
			fields := make(map[string]string)
			for name, p := range parts {
				if p.fileName != "" {
					t.Errorf("field %q is a file part, %q", name, p.fileName)
				}
				fields[name] = string(p.body)
			}
			if !maps.Equal(fields, tc.fields) {
				t.Errorf("fields = %v, want %v", fields, tc.fields)
			}
		})
	}
}

func TestTranscribeDecodesReply(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, transcribeReply)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	got, err := c.Transcribe(t.Context(), client.TranscriptionRequest{
		Model: "m", File: strings.NewReader("RIFF"), FileName: "clip.wav",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	want := client.Transcription{
		Text:  "The access code is 7429.",
		Usage: client.TranscriptionUsage{InputTokens: 174, OutputTokens: 11, TotalTokens: 185},
	}
	if *got != want {
		t.Errorf("Transcribe = %+v, want %+v", *got, want)
	}
}

func TestTranscribeRefusesReply(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, `{"text":7429}`)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	got, err := c.Transcribe(t.Context(), client.TranscriptionRequest{
		Model: "m", File: strings.NewReader("RIFF"), FileName: "clip.wav",
	})
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Errorf("Transcribe = %+v, %v; want a decode response error", got, err)
	}
}

// TestTranscribeRefusesRequest covers what Transcribe refuses, and when: before the token source
// is called or anything is sent.
func TestTranscribeRefusesRequest(t *testing.T) {
	file := strings.NewReader("RIFF")
	cases := []struct {
		name string
		req  client.TranscriptionRequest
		want string
	}{
		{"no model", client.TranscriptionRequest{File: file, FileName: "clip.wav"}, "no model"},
		{"no file", client.TranscriptionRequest{Model: "m", FileName: "clip.wav"}, "no file"},
		{"no file name", client.TranscriptionRequest{Model: "m", File: file}, "no file name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := clienttest.NewServer(t, http.StatusOK, transcribeReply)
			called := false
			c := newClient(t, client.Config{
				BaseURL: srv.URL,
				Token:   func(context.Context) (string, error) { called = true; return "", nil },
			})
			got, err := c.Transcribe(t.Context(), tc.req)
			if err == nil {
				t.Fatalf("Transcribe = %+v, nil; want an error containing %q", got, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Transcribe error = %q, want it to contain %q", err, tc.want)
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

// TestTranscribeAPIError covers a non-2xx reply: an *APIError with the OpenAI-style error body's
// details, whatever format the Client speaks.
func TestTranscribeAPIError(t *testing.T) {
	reply := `{"error":{"code":400,"message":"Only 'json' response_format is supported for transcription",` +
		`"type":"invalid_request_error"}}`
	srv := clienttest.NewServer(t, http.StatusBadRequest, reply)
	c := newClient(t, client.Config{BaseURL: srv.URL})
	got, err := c.Transcribe(t.Context(), client.TranscriptionRequest{
		Model: "m", File: strings.NewReader("RIFF"), FileName: "clip.wav",
	})
	apiErr, ok := errors.AsType[*client.APIError](err)
	if !ok {
		t.Fatalf("Transcribe = %+v, %v; want an *APIError", got, err)
	}
	if apiErr.Status != http.StatusBadRequest ||
		apiErr.Message != "Only 'json' response_format is supported for transcription" ||
		apiErr.Type != "invalid_request_error" || apiErr.Code != "400" || string(apiErr.Body) != reply {
		t.Errorf("APIError = %+v, want 400 with the body's message, type, and code", apiErr)
	}
}

func TestTranscribeTokenErrorSendsNothing(t *testing.T) {
	srv := clienttest.NewServer(t, http.StatusOK, transcribeReply)
	errExpired := errors.New("token expired")
	c := newClient(t, client.Config{
		BaseURL: srv.URL,
		Token:   func(context.Context) (string, error) { return "", errExpired },
	})
	_, err := c.Transcribe(t.Context(), client.TranscriptionRequest{
		Model: "m", File: bytes.NewReader(clip(64 << 10)), FileName: "clip.wav",
	})
	if !errors.Is(err, errExpired) {
		t.Errorf("Transcribe error = %v, want it to wrap %v", err, errExpired)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
	}
}

// drainServer starts a server that reads each request's body to its end or its failure, which
// it ignores, as the upload's failure is the test's subject, and closes arrived on the first
// request's arrival, once its header is read and before its body is.
func drainServer(t *testing.T) (url string, arrived <-chan struct{}) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(ch) })
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, transcribeReply)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, ch
}

func TestTranscribeReaderError(t *testing.T) {
	base, _ := drainServer(t)
	c := newClient(t, client.Config{BaseURL: base})
	errBroken := errors.New("disk read failed")
	got, err := c.Transcribe(t.Context(), client.TranscriptionRequest{
		Model:    "m",
		File:     io.MultiReader(bytes.NewReader(clip(64<<10)), iotest.ErrReader(errBroken)),
		FileName: "clip.wav",
	})
	if !errors.Is(err, errBroken) {
		t.Errorf("Transcribe = %+v, %v; want an error wrapping %v", got, err, errBroken)
	}
}

// stallingReader is a clip, such as a slow file, that gives one chunk, then, once the request
// has reached the server, stalls until ctx ends, and gives another chunk: a clip that never
// fails, so the upload's failure is the request's alone.
type stallingReader struct {
	ctx     context.Context
	arrived <-chan struct{}
	// stalled is closed when the reader stalls, the clip partly sent.
	stalled chan struct{}
	// ended is set when the stalled Read returns.
	ended atomic.Bool
	reads int
}

func (r *stallingReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads == 2 {
		<-r.arrived
		close(r.stalled)
		<-r.ctx.Done()
		r.ended.Store(true)
	}
	return copy(p, clip(len(p))), nil
}

// TestTranscribeCancelMidUpload cancels the context while the clip is partly uploaded: the
// request has reached the server, so the upload streams rather than buffering the clip, and
// Transcribe returns ctx's error promptly, only after the upload stopped reading the clip. The
// clip itself never fails, so the error is the cancellation's, not the upload's write into the
// pipe the cancelled request closed.
func TestTranscribeCancelMidUpload(t *testing.T) {
	base, arrived := drainServer(t)
	c := newClient(t, client.Config{BaseURL: base})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	file := &stallingReader{ctx: ctx, arrived: arrived, stalled: make(chan struct{})}
	type result struct {
		t   *client.Transcription
		err error
		// ended is whether the clip's stalled Read had returned when Transcribe did.
		ended bool
	}
	done := make(chan result, 1)
	go func() {
		got, err := c.Transcribe(ctx, client.TranscriptionRequest{Model: "m", File: file, FileName: "clip.wav"})
		done <- result{got, err, file.ended.Load()}
	}()
	select {
	case <-file.stalled:
	case res := <-done:
		t.Fatalf("Transcribe = %+v, %v before the upload stalled", res.t, res.err)
	case <-time.After(waitLimit):
		t.Fatal("the upload never reached the server with the clip partly read")
	}
	cancel()
	select {
	case res := <-done:
		if !errors.Is(res.err, context.Canceled) {
			t.Errorf("Transcribe = %+v, %v; want an error wrapping context.Canceled", res.t, res.err)
		}
		if !res.ended {
			t.Error("Transcribe returned while the upload was still reading the clip")
		}
	case <-time.After(waitLimit):
		t.Fatalf("Transcribe didn't return within %v of the cancellation", waitLimit)
	}
}
