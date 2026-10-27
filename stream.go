package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"

	"github.com/JaimeStill/spike-ai-pipeline-client/internal/sse"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// GenerateStream streams req's response as the spec's streaming events, which a
// protocol.Accumulator folds into the Response Generate would return. The sequence is lazy:
// nothing is validated, encoded, or sent until it is ranged, and each range sends a new request.
// It asks for text/event-stream and reads the events as they arrive, inside the range loop, with
// no goroutine; breaking out of the loop closes the reply's body.
//
// Each pair holds an event or an error, never both. The events run from
// protocol.ResponseCreated to one terminal event, after which the sequence ends. A failure the
// endpoint reports in the stream, such as llama.cpp's error frame, data {"error": {...}}, is no
// error pair: the stream ends as the spec has it, with protocol.ErrorEvent, holding the error's
// type, code, message, and param, and then the terminal protocol.ResponseFailed, whose Response
// is in protocol.StatusFailed, its Error the failure, holding the output made before it. The
// caller checks the folded Response's Status for StatusFailed as it does for StatusIncomplete.
//
// Error pairs are for a stream that breaks, or never begins. An error ends the sequence as its
// last pair, and no event follows it; the events before it fold, in a protocol.Accumulator, to
// the partial Response, in protocol.StatusInProgress:
//
//   - a request Validate or the format refuses, a token source error, a transport failure, or a
//     non-2xx reply, the same *APIError as Generate, is the sequence's only pair
//   - a cancelled or expired ctx is ctx's error, wrapped, matching context.Canceled or
//     context.DeadlineExceeded; cancelling closes the connection, which stops the generation,
//     and no event is yielded once ctx is done
//   - a stream that ends before its terminal event matches io.ErrUnexpectedEOF, as does a read
//     that fails, such as on a connection dropped or reset, whose error is wrapped too
//   - an event the format can't decode is the format's error, wrapped
//
// Comments, such as llama.cpp's ping, a ":" line every 30 seconds, are no events and change
// nothing.
func (c *Client) GenerateStream(ctx context.Context, req *protocol.Request) iter.Seq2[protocol.Event, error] {
	return func(yield func(protocol.Event, error) bool) {
		if err := c.stream(ctx, req, yield); err != nil {
			yield(nil, fmt.Errorf("client: generate stream: %w", err))
		}
	}
}

// stream sends req and yields its stream's events, returning the error that ends the stream,
// or nil once the terminal event is yielded or the caller stops the range.
func (c *Client) stream(ctx context.Context, req *protocol.Request, yield func(protocol.Event, error) bool) error {
	if err := req.Validate(); err != nil {
		return err
	}
	header := make(http.Header)
	body, err := c.format.EncodeStreamRequest(req, header)
	if err != nil {
		return err
	}
	resp, err := c.send(ctx, c.format.Route(), header, bytes.NewReader(body), "application/json", "text/event-stream",
		c.format.DecodeError)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	// emit yields made, reporting whether the stream is over: the caller stopped, or its
	// terminal event is yielded. Once ctx is done it yields nothing and returns ctx's error.
	emit := func(made []protocol.Event) (bool, error) {
		for _, event := range made {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if !yield(event, nil) || terminal(event) {
				return true, nil
			}
		}
		return false, nil
	}
	events, decoder := sse.NewDecoder(resp.Body), c.format.NewStreamDecoder()
	for {
		e, err := events.Next()
		if errors.Is(err, io.EOF) {
			made, err := decoder.End()
			switch over, ctxErr := emit(made); {
			case ctxErr != nil:
				return ctxErr
			case over:
				return nil
			case err != nil:
				return err
			}
			return fmt.Errorf("stream ended before its terminal event: %w", io.ErrUnexpectedEOF)
		}
		if err != nil {
			return readError(ctx, err)
		}
		// The decoder takes the data alone: a format that names its events carries the name in
		// the data too, so the event field is the framing's and isn't passed.
		made, err := decoder.Decode([]byte(e.Data))
		switch over, ctxErr := emit(made); {
		case ctxErr != nil:
			return ctxErr
		case over:
			return nil
		case err != nil:
			return err
		}
	}
}

// readError is the error that ends a stream whose body failed to read with err. Once ctx is
// done the transport has closed the connection, so the read failed because of ctx, and its
// error is ctx's. Otherwise the stream ended before its terminal event: the error matches
// io.ErrUnexpectedEOF, which the transport returns for a connection dropped mid-body, and also
// err, such as a connection reset, which it returns for one broken another way.
func readError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("read stream: %w", err)
	}
	return fmt.Errorf("read stream: %w: %w", io.ErrUnexpectedEOF, err)
}

// terminal reports whether e is a terminal event, the last of a stream.
func terminal(e protocol.Event) bool {
	switch e.(type) {
	case protocol.ResponseCompleted, protocol.ResponseIncomplete, protocol.ResponseFailed:
		return true
	}
	return false
}
