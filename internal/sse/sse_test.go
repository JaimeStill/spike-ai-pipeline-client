package sse_test

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/JaimeStill/spike-ai-pipeline-client/internal/sse"
)

// readAll decodes every event of r until Next returns an error, failing t unless that error is
// io.EOF.
func readAll(t *testing.T, r io.Reader) ([]sse.Event, *sse.Decoder) {
	t.Helper()
	d := sse.NewDecoder(r)
	var got []sse.Event
	for {
		e, err := d.Next()
		if errors.Is(err, io.EOF) {
			return got, d
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		got = append(got, e)
	}
}

// splitReader returns its chunks one per Read, so a test controls where reads break.
type splitReader struct{ chunks []string }

func (r *splitReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if r.chunks[0] == "" {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

var parseTests = []struct {
	name   string
	stream string
	want   []sse.Event
}{
	{"LF endings", "data: a\n\ndata: b\n\n", []sse.Event{{Data: "a"}, {Data: "b"}}},
	{"CR endings", "data: a\r\rdata: b\r\r", []sse.Event{{Data: "a"}, {Data: "b"}}},
	{"CRLF endings", "data: a\r\n\r\ndata: b\r\n\r\n", []sse.Event{{Data: "a"}, {Data: "b"}}},
	{"mixed endings", "data: a\r\n\ndata: b\r\r\ndata: c\n\r", []sse.Event{{Data: "a"}, {Data: "b"}, {Data: "c"}}},
	{"CR CR is two line endings", "data: a\r\rdata: b\n\n", []sse.Event{{Data: "a"}, {Data: "b"}}},
	{"leading BOM stripped", "\uFEFFdata: a\n\n", []sse.Event{{Data: "a"}}},
	{"leading BOM before a blank line", "\uFEFF\ndata: a\n\n", []sse.Event{{Data: "a"}}},
	{"only one BOM stripped", "\uFEFF\uFEFFdata: a\n\n", nil},
	{"BOM after the start kept", "data: a\n\n\uFEFFdata: b\n\n", []sse.Event{{Data: "a"}}},
	{"comment ignored", ": ping\ndata: a\n: more\n\n", []sse.Event{{Data: "a"}}},
	{"comment-only block dispatches nothing", ": ping\n\n: ping\n\n", nil},
	{"no colon is a name with empty value", "data\n\n", []sse.Event{{Data: ""}}},
	{"no colon data lines joined", "data\ndata\n\n", []sse.Event{{Data: "\n"}}},
	{"one leading space removed", "data:  a\n\n", []sse.Event{{Data: " a"}}},
	{"no space after the colon", "data:a\n\n", []sse.Event{{Data: "a"}}},
	{"value split at the first colon", "data: a: b\n\n", []sse.Event{{Data: "a: b"}}},
	{"trailing space kept", "data: a \n\n", []sse.Event{{Data: "a "}}},
	{"data lines joined", "data: a\ndata: b\ndata\ndata: c\n\n", []sse.Event{{Data: "a\nb\n\nc"}}},
	{"empty data dispatched", "data:\n\n", []sse.Event{{Data: ""}}},
	{"event names the type", "event: response.created\ndata: {}\n\n", []sse.Event{{Type: "response.created", Data: "{}"}}},
	{"last event field wins", "event: a\nevent: b\ndata: x\n\n", []sse.Event{{Type: "b", Data: "x"}}},
	{"type resets after dispatch", "event: a\ndata: x\n\ndata: y\n\n", []sse.Event{{Type: "a", Data: "x"}, {Data: "y"}}},
	{"type resets after a block without data", "event: a\n\ndata: y\n\n", []sse.Event{{Data: "y"}}},
	{"id set", "id: 7\ndata: x\n\n", []sse.Event{{Data: "x", ID: "7"}}},
	{"id persists", "id: 7\ndata: x\n\ndata: y\n\n", []sse.Event{{Data: "x", ID: "7"}, {Data: "y", ID: "7"}}},
	{"id set by a block without data", "id: 7\n\ndata: y\n\n", []sse.Event{{Data: "y", ID: "7"}}},
	{"empty id resets", "id: 7\ndata: x\n\nid\ndata: y\n\n", []sse.Event{{Data: "x", ID: "7"}, {Data: "y"}}},
	{"id with NUL ignored", "id: 7\ndata: x\n\nid: 8\x009\ndata: y\n\n", []sse.Event{{Data: "x", ID: "7"}, {Data: "y", ID: "7"}}},
	{"unknown fields ignored", "foo: bar\nData: no\ndata: x\nretry: 5\n\n", []sse.Event{{Data: "x"}}},
	{"blank line without data dispatches nothing", "\n\n\nevent: a\n\n", nil},
	{"pending event at end discarded", "data: a\n\ndata: b\n", []sse.Event{{Data: "a"}}},
	{"pending line at end discarded", "data: a\n\ndata: b", []sse.Event{{Data: "a"}}},
	{"empty stream", "", nil},
	// Multi-byte text, which the one-byte reader splits mid-character.
	{"multi-byte text kept", "event: é\nid: €\ndata: h\u00e9llo \u20ac \U0001D11E\n\n", []sse.Event{{Type: "é", Data: "héllo € \U0001D11E", ID: "€"}}},
	{"invalid byte in data replaced", "data: a\xffb\n\n", []sse.Event{{Data: "a\uFFFDb"}}},
	{"invalid byte in event type replaced", "event: a\x80\ndata: x\n\n", []sse.Event{{Type: "a\uFFFD", Data: "x"}}},
	{"invalid byte in id replaced", "id: 7\xfe\ndata: x\n\n", []sse.Event{{Data: "x", ID: "7\uFFFD"}}},
	{"truncated sequence is one replacement", "data: a\xe2\x82b\n\n", []sse.Event{{Data: "a\uFFFDb"}}},
	{"truncated sequence at line end", "data: a\xf0\x9f\x98\ndata: b\xe2\r\n\r\n", []sse.Event{{Data: "a\uFFFD\nb\uFFFD"}}},
	{"overlong and surrogate forms replaced per byte", "data: \xc0\x80\xed\xa0\x80\n\n", []sse.Event{{Data: "\uFFFD\uFFFD\uFFFD\uFFFD\uFFFD"}}},
	// The Unicode Standard's example of maximal-subpart replacement (Table 3-8).
	{"maximal subparts replaced", "data: a\xf1\x80\x80\xe1\x80\xc2b\x80c\x80\xbfd\n\n", []sse.Event{{Data: "a\uFFFD\uFFFD\uFFFDb\uFFFDc\uFFFD\uFFFDd"}}},
}

func TestNext(t *testing.T) {
	readers := []struct {
		name string
		wrap func(string) io.Reader
	}{
		{"whole", func(s string) io.Reader { return strings.NewReader(s) }},
		{"one byte", func(s string) io.Reader { return iotest.OneByteReader(strings.NewReader(s)) }},
		{"split after CR", func(s string) io.Reader {
			// Each read ends just after a CR, so every CRLF is split across two reads.
			var chunks []string
			for s != "" {
				i := strings.IndexByte(s, '\r')
				if i < 0 {
					i = len(s) - 1
				}
				chunks = append(chunks, s[:i+1])
				s = s[i+1:]
			}
			return &splitReader{chunks: chunks}
		}},
	}
	for _, tt := range parseTests {
		for _, r := range readers {
			t.Run(tt.name+"/"+r.name, func(t *testing.T) {
				got, _ := readAll(t, r.wrap(tt.stream))
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("events = %q\nwant     %q", got, tt.want)
				}
			})
		}
	}
}

func TestRetry(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		want   time.Duration
		ok     bool
	}{
		{"unset", "data: x\n\n", 0, false},
		{"digits", "retry: 1500\n", 1500 * time.Millisecond, true},
		{"zero", "retry: 0\n", 0, true},
		{"last valid wins", "retry: 10\nretry: 20\nretry: x\n", 20 * time.Millisecond, true},
		{"sign refused", "retry: +10\n", 0, false},
		{"space inside refused", "retry: 1 0\n", 0, false},
		{"fraction refused", "retry: 1.5\n", 0, false},
		{"empty refused", "retry\n", 0, false},
		{"overflow refused", "retry: 99999999999999999999\n", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, d := readAll(t, strings.NewReader(tt.stream))
			got, ok := d.Retry()
			if got != tt.want || ok != tt.ok {
				t.Errorf("Retry() = %v, %v; want %v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// TestNextDispatchesAsBytesArrive feeds a stream through a pipe and checks that each event is
// returned as soon as its blank line arrives, without waiting for the next byte: in particular a
// CR-terminated blank line, whose ending could be the first half of a CRLF.
func TestNextDispatchesAsBytesArrive(t *testing.T) {
	pr, pw := io.Pipe()
	d := sse.NewDecoder(pr)
	writes := []struct {
		chunk string
		want  sse.Event
	}{
		{"data: a\r\r", sse.Event{Data: "a"}},
		{"\ndata: b\r\n\r", sse.Event{Data: "b"}},
		{"\nevent: c\ndata: c\n\n", sse.Event{Type: "c", Data: "c"}},
	}
	type result struct {
		e   sse.Event
		err error
	}
	for _, w := range writes {
		// The chunk is written only once the event before it has been returned, so a Next that
		// read past its blank line would wait for bytes that never come.
		go func() { _, _ = io.WriteString(pw, w.chunk) }()
		done := make(chan result, 1)
		go func() {
			e, err := d.Next()
			done <- result{e, err}
		}()
		select {
		case r := <-done:
			if r.err != nil {
				t.Fatalf("Next: %v", r.err)
			}
			if r.e != w.want {
				t.Errorf("Next() = %q, want %q", r.e, w.want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Next waited past the blank line ending %q", w.chunk)
		}
	}
	_ = pw.Close()
	if _, err := d.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("Next at end = %v, want io.EOF", err)
	}
}

func TestNextReadError(t *testing.T) {
	boom := errors.New("boom")
	d := sse.NewDecoder(io.MultiReader(strings.NewReader("data: a\n\ndata: b\n"), iotest.ErrReader(boom)))
	if e, err := d.Next(); err != nil || e.Data != "a" {
		t.Fatalf("Next() = %q, %v; want a, nil", e, err)
	}
	if _, err := d.Next(); !errors.Is(err, boom) {
		t.Errorf("Next() error = %v, want %v", err, boom)
	}
}
