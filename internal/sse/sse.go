// Package sse is a decoder for a server-sent event stream, the text/event-stream body of a
// streamed reply, written to the WHATWG HTML standard's rules for parsing and interpreting an
// event stream. It reads the body as the bytes arrive and holds only the event being built. It is
// a decoder alone: it never reconnects, and it never sends Last-Event-ID.
//
// Its exported identifiers:
//
//   - [Event] is one dispatched event: its type, data, and last event ID.
//   - [Decoder] reads events from a stream, one per call.
//   - [NewDecoder] returns a Decoder reading a stream.
//   - [Decoder.Next] returns the stream's next event, or io.EOF at its end.
//   - [Decoder.Retry] is the reconnection time the stream last set, if it set one.
package sse

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
	"strings"
	"time"
)

// Event is one event a stream dispatched.
type Event struct {
	// Type is the event's type, from its last event field. Empty means the stream named none,
	// which the standard reads as "message".
	Type string
	// Data is the event's data: its data fields' values joined with "\n". It may be empty, for
	// an event whose only data field had an empty value.
	Data string
	// ID is the stream's last event ID when the event was dispatched: the last id field's
	// value, in this event or an earlier one, that held no NUL. Empty means none was set, or
	// the stream reset it.
	ID string
}

// Decoder reads the events of one stream. It isn't safe for concurrent use.
//
// A line has no length limit: the Decoder holds a line until its ending arrives, so a stream's
// longest line is the most it buffers. The caller bounds a stream it doesn't trust with its
// context or transport, as it bounds the rest of a reply.
type Decoder struct {
	r *bufio.Reader
	// first is true until the first line is read, whose leading BOM is stripped.
	first bool
	// afterCR is true when the last line ended in CR, so an LF that starts the next read
	// completes that line ending rather than ending an empty line. It is carried between reads
	// rather than settled by peeking past the CR, which would wait for a byte the server may
	// not send until its next event.
	afterCR bool
	line    []byte

	// The buffers the standard names: the event being built, and the last event ID.
	typ    string
	data   strings.Builder
	lastID string

	retry    time.Duration
	hasRetry bool
}

// NewDecoder returns a Decoder reading the stream r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r), first: true}
}

// Next reads the stream until it dispatches an event, at a blank line ending a block that holds
// a data field, and returns that event. Comments, unknown fields, and blocks without data
// dispatch nothing, and Next reads on past them. At the stream's end it returns io.EOF, and the
// standard discards an event that no blank line ended. A read error is returned as it is.
func (d *Decoder) Next() (Event, error) {
	for {
		line, err := d.readLine()
		if err != nil {
			return Event{}, err
		}
		if len(line) == 0 {
			if e, ok := d.dispatch(); ok {
				return e, nil
			}
			continue
		}
		d.field(line)
	}
}

// Retry returns the reconnection time the stream's last valid retry field set, and whether one
// did. The Decoder never reconnects; the value is reported only.
func (d *Decoder) Retry() (time.Duration, bool) {
	return d.retry, d.hasRetry
}

// readLine returns the stream's next line without its ending: CRLF, LF, or CR. A line cut off by
// the stream's end is discarded, as the standard discards the event it belongs to, and io.EOF
// returned. The slice is valid until the next call.
func (d *Decoder) readLine() ([]byte, error) {
	d.line = d.line[:0]
	for {
		b, err := d.r.ReadByte()
		if err != nil {
			return nil, err
		}
		if d.afterCR {
			d.afterCR = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\r':
			d.afterCR = true
			return d.stripBOM(d.line), nil
		case '\n':
			return d.stripBOM(d.line), nil
		}
		d.line = append(d.line, b)
	}
}

// bom is the UTF-8 byte order mark, which the standard strips from a stream's start.
var bom = []byte("\uFEFF")

// stripBOM removes one leading BOM from the stream's first line. A BOM holds no CR or LF, so a
// stream's leading BOM is always the first line's.
func (d *Decoder) stripBOM(line []byte) []byte {
	if d.first {
		d.first = false
		return bytes.TrimPrefix(line, bom)
	}
	return line
}

// field processes one line that isn't blank: a comment, which starts with a colon, or a field,
// split at its first colon into name and value with one leading space removed from the value. A
// line without a colon is a field with an empty value.
func (d *Decoder) field(line []byte) {
	if line[0] == ':' {
		return
	}
	name, value, _ := bytes.Cut(line, []byte(":"))
	value = bytes.TrimPrefix(value, []byte(" "))
	switch string(name) {
	case "event":
		d.typ = string(value)
	case "data":
		d.data.Write(value)
		d.data.WriteByte('\n')
	case "id":
		if bytes.IndexByte(value, 0) < 0 {
			d.lastID = string(value)
		}
	case "retry":
		if ms, ok := digits(value); ok {
			d.retry, d.hasRetry = time.Duration(ms)*time.Millisecond, true
		}
	}
}

// digits parses value as a retry field's milliseconds: one or more ASCII digits and nothing
// else. A value too large for a time.Duration is refused, as a non-digit value is.
func digits(value []byte) (int64, bool) {
	if len(value) == 0 {
		return 0, false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	ms, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil || ms > int64(time.Duration(1<<63-1)/time.Millisecond) {
		return 0, false
	}
	return ms, true
}

// dispatch ends the block a blank line closes. It returns the block's event if its data buffer
// holds anything, with the trailing newline the data fields appended removed, and resets the
// type and data buffers either way. The last event ID persists across blocks.
func (d *Decoder) dispatch() (Event, bool) {
	data, typ := d.data.String(), d.typ
	d.data.Reset()
	d.typ = ""
	if data == "" {
		return Event{}, false
	}
	return Event{Type: typ, Data: strings.TrimSuffix(data, "\n"), ID: d.lastID}, true
}
