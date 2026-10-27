package format

import (
	"net/http"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// Format is a wire format: the adapter between the protocol's request and response model and one
// API's shape, such as Chat Completions. A client.Client speaks one Format, chosen at the caller's
// composition root by the implementing package's constructor; there is no registry. Each
// implementation lives in a package of its own below this one, named for the endpoint it
// targets, such as completions.
//
// The client validates a request with protocol.Request.Validate before calling EncodeRequest
// or EncodeStreamRequest, and sends nothing when either fails, so a Format refuses there what
// its API can't express. A Format is safe for concurrent use: the state of a stream is its
// StreamDecoder's, and a new one serves each stream.
type Format interface {
	// Route is the path, relative to the client's base URL, that a request is posted to, such
	// as "chat/completions".
	Route() string
	// EncodeRequest encodes req as the JSON body of a request, merging req.Native into its top
	// level. It may add the format's own headers to header, such as a version header its API
	// requires; the client sets Content-Type, Accept, and Authorization itself. It refuses an
	// item the format can't express and a Native member that clashes with a member req's
	// typed fields set.
	EncodeRequest(req *protocol.Request, header http.Header) ([]byte, error)
	// DecodeResponse decodes the body of a 2xx reply into a Response.
	DecodeResponse(body []byte) (*protocol.Response, error)
	// EncodeStreamRequest encodes req as the JSON body of a streamed request, as EncodeRequest
	// encodes it for an unstreamed one, with the members that ask its API to stream the reply.
	// It refuses what EncodeRequest refuses, and a Native member that clashes with those.
	EncodeStreamRequest(req *protocol.Request, header http.Header) ([]byte, error)
	// NewStreamDecoder returns a decoder for the reply to one streamed request. The client
	// calls it once per stream, so the decoder's state is that stream's alone.
	NewStreamDecoder() StreamDecoder
	// DecodeError returns the error message, type, and code the body of a non-2xx reply holds,
	// each empty when the body doesn't give it. The client puts them in its APIError.
	DecodeError(body []byte) (message, typ, code string)
}

// StreamDecoder turns the events of one streamed reply into the protocol's streaming events: a
// stream opens with protocol.ResponseCreated and closes with one terminal event, whose Response
// holds the finished output and the usage. It is framing-neutral: the client reads the reply's
// framing, such as server-sent events, and hands the decoder each event's type and data. It
// serves one stream, and isn't safe for concurrent use.
type StreamDecoder interface {
	// Decode decodes the stream's next event, of type typ, empty when the framing names none,
	// and data, and returns the protocol events it makes, which may be none. A chunk the format
	// can't decode is an error, after which the stream is broken; with it, Decode returns the
	// events it made before the error, which the client yields ahead of it.
	Decode(typ string, data []byte) ([]protocol.Event, error)
	// End is told the stream has ended, and returns the events that close it. A stream that
	// ended before it was complete is an error matching io.ErrUnexpectedEOF.
	End() ([]protocol.Event, error)
}
