// Package client is the model client of spike-ai-pipeline: Go primitives over the inference
// engines' native APIs, with the wire format a pluggable layer beneath a stable request and
// response model. README.md states its role in the spike.
//
// The module is four layers, importing one way. Package protocol is the model, Open Responses'
// item model in its stateless subset; package format is the contract a wire format meets; each
// wire format is a package below it, named for the endpoint it targets, format/completions,
// format/messages, and format/responses; and this package is the transport, which sends a
// protocol.Request in the format chosen by its constructor at the caller's composition root.
// Embeddings and transcription sit beside the protocol, outside the Format, on the
// OpenAI-compatible shape whatever format the Client speaks.
//
// The library sets no policy of its own: no default model, token budget, timeout, retry, or
// size limit. The caller's http.Client and context carry the timeouts, and retry is opt-in, the
// RetryPolicy the caller sets in Config.
//
// Its exported identifiers:
//
//   - [Config] describes an endpoint: base URL, query, token source, http.Client, format, and
//     retry policy.
//   - [TokenSource] returns the bearer token for one request.
//   - [New] validates a Config and returns a Client.
//   - [RetryPolicy] is the caller's opt-in policy for sending a request again: its most
//     attempts and the backoff's base and most delays.
//   - [Client] makes requests of one endpoint in one format.
//   - [Client.Generate] sends a Request and returns its Response.
//   - [Client.GenerateStream] sends a Request when ranged and yields its response's streaming
//     events.
//   - [Client.Embed] sends an *EmbeddingRequest to the OpenAI-compatible embeddings endpoint.
//   - [EmbeddingRequest] is an embedding model, its inputs, and a native-tier field.
//   - [Embeddings] is one vector per input, in input order, and usage.
//   - [EmbeddingUsage] counts the tokens an embedding request took.
//   - [Client.Transcribe] streams a *TranscriptionRequest's clip to the OpenAI-compatible
//     transcription endpoint as a multipart upload.
//   - [TranscriptionRequest] is an audio model, the clip as a reader and its file name, the
//     optional language and prompt, and a native-tier field of extra form fields.
//   - [Transcription] is a clip's transcript and usage.
//   - [TranscriptionUsage] counts the tokens a transcription took.
//   - [APIError] is a non-2xx reply, matched with errors.As.
package client
