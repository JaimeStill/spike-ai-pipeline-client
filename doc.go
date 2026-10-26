// Package client is the model client of spike-ai-pipeline: Go primitives over the inference
// engines' native APIs, with the wire format a pluggable layer beneath a stable request and
// response model. README.md states its role in the spike.
//
// The module is four layers, importing one way. Package protocol is the model, Open Responses'
// item model in its stateless subset; package format is the contract a wire format meets; each
// wire format is a package below it, named for the endpoint it targets, such as
// format/completions; and this package is the transport, which sends a protocol.Request in the
// format chosen by its constructor at the caller's composition root.
//
// The library ships no policy: no default model, token budget, timeout, retry, or size limit.
// The caller's http.Client and context carry them.
//
// Its exported identifiers:
//
//   - [Config] describes an endpoint: base URL, query, token source, http.Client, and format.
//   - [TokenSource] returns the bearer token for one request.
//   - [New] validates a Config and returns a Client.
//   - [Client] makes requests of one endpoint in one format.
//   - [Client.Respond] sends a Request and returns its Response.
//   - [APIError] is a non-2xx reply, matched with errors.As.
package client
