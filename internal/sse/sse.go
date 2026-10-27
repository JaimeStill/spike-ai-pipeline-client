// Package sse is a decoder for a server-sent event stream, the text/event-stream body of a
// streamed reply, written to the WHATWG HTML standard's rules for parsing and interpreting an
// event stream. It reads the body as the bytes arrive and holds only the event being built. It is
// a decoder alone: it never reconnects, and it never sends Last-Event-ID.
//
// The stream is decoded as UTF-8 with replacement, as the standard requires: each invalid byte
// sequence becomes one U+FFFD per maximal subpart, as the Encoding Standard's UTF-8 decoder
// yields, so Event's fields are always valid UTF-8.
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
	"unicode/utf8"
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

// Decoder reads the events of one stream. It isn't safe for concurrent use. It decodes the
// stream as UTF-8 with replacement, as the standard does.
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
		d.field(decodeUTF8(line))
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

// decodeUTF8 returns line decoded as UTF-8 with replacement, by the Encoding Standard's UTF-8
// decoder: each maximal subpart of an invalid sequence, the longest prefix of a well-formed
// sequence, becomes one U+FFFD, and the byte that ended it is decoded afresh. A sequence cut off
// by the line's end is one U+FFFD. Decoding a line at a time decodes the stream: a line ends at
// CR or LF, and an ASCII byte ends any pending sequence in the whole-stream decoder too, so a
// character split across reads is whole by the time its line is decoded. A valid line, the usual
// one, is returned as it is.
//
// This is not utf8.DecodeRune's replacement, which steps one byte per invalid byte, so a
// truncated sequence gives a U+FFFD per byte; nor bytes.ToValidUTF8's, which collapses a run of
// invalid bytes into one.
func decodeUTF8(line []byte) []byte {
	if utf8.Valid(line) {
		return line
	}
	out := make([]byte, 0, len(line)+2*utf8.UTFMax)
	var (
		cp           rune
		needed, seen int
		lower, upper byte = 0x80, 0xBF
	)
	for i := 0; i < len(line); {
		b := line[i]
		if needed == 0 {
			i++
			switch {
			case b <= 0x7F:
				out = append(out, b)
			case 0xC2 <= b && b <= 0xDF:
				needed, cp = 1, rune(b&0x1F)
			case 0xE0 <= b && b <= 0xEF:
				// E0 bars overlong forms; ED bars the surrogates.
				switch b {
				case 0xE0:
					lower = 0xA0
				case 0xED:
					upper = 0x9F
				}
				needed, cp = 2, rune(b&0x0F)
			case 0xF0 <= b && b <= 0xF4:
				// F0 bars overlong forms; F4 bars code points past U+10FFFF.
				switch b {
				case 0xF0:
					lower = 0x90
				case 0xF4:
					upper = 0x8F
				}
				needed, cp = 3, rune(b&0x07)
			default:
				out = utf8.AppendRune(out, utf8.RuneError)
			}
			continue
		}
		if b < lower || b > upper {
			// The maximal subpart ends here; b is not consumed, and starts afresh.
			out = utf8.AppendRune(out, utf8.RuneError)
			cp, needed, seen, lower, upper = 0, 0, 0, 0x80, 0xBF
			continue
		}
		i++
		lower, upper = 0x80, 0xBF
		cp = cp<<6 | rune(b&0x3F)
		if seen++; seen == needed {
			out = utf8.AppendRune(out, cp)
			cp, needed, seen = 0, 0, 0
		}
	}
	if needed != 0 {
		out = utf8.AppendRune(out, utf8.RuneError)
	}
	return out
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
