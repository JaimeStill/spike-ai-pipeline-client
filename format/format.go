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
// The client validates a request with protocol.Request.Validate before calling EncodeRequest,
// and sends nothing when EncodeRequest fails, so a Format refuses there what its API can't
// express.
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
	// DecodeError returns the error message, type, and code the body of a non-2xx reply holds,
	// each empty when the body doesn't give it. The client puts them in its APIError.
	DecodeError(body []byte) (message, typ, code string)
}
