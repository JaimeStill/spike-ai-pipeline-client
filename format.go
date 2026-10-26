package client

import "net/http"

// Format is a wire format: the adapter between the client's request and response model and one
// API's shape, such as Chat Completions. A Client speaks one Format, chosen at the caller's
// composition root by the format package's constructor; there is no registry. Formats live in
// their own packages, named for the API they target.
//
// The Client validates a request against the model before calling EncodeRequest, and sends
// nothing when EncodeRequest fails, so a Format refuses there what its API can't express.
type Format interface {
	// Route is the path, relative to Config.BaseURL, that a request is posted to, such as
	// "chat/completions".
	Route() string
	// EncodeRequest encodes req as the JSON body of a request, merging req.Native into its top
	// level. It may add the format's own headers to header, such as a version header its API
	// requires; the Client sets Content-Type, Accept, and Authorization itself. It refuses an
	// item the format can't express and a Native member that clashes with a member req's
	// typed fields set.
	EncodeRequest(req *Request, header http.Header) ([]byte, error)
	// DecodeResponse decodes the body of a 2xx reply into a Response.
	DecodeResponse(body []byte) (*Response, error)
	// DecodeError returns the error message, type, and code the body of a non-2xx reply holds,
	// each empty when the body doesn't give it. The Client puts them in an *APIError.
	DecodeError(body []byte) (message, typ, code string)
}
