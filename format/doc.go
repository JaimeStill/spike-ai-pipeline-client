// Package format is the contract a wire format meets: [Format] encodes the protocol package's
// request in one API's wire shape and decodes the reply into its response, whole or streamed as
// the protocol's events. It imports only protocol; each implementation is a package below it,
// named for the endpoint it targets, such as completions for Chat Completions, and none imports
// the client.
//
// It holds the contract and what the formats share: the OpenAI-style error body, which every
// OpenAI-compatible endpoint answers with, so completions decodes its errors with it, and so
// does the client for its operations outside the Format, such as embeddings; and the
// StreamError a stream decoder returns for an error frame, which the client yields as it is.
//
// Its exported identifiers:
//
//   - [Format] is a wire format, the adapter between the protocol and one API's shape.
//   - [StreamDecoder] turns one streamed reply's events into the protocol's streaming events.
//   - [DecodeOpenAIError] decodes an OpenAI-style error body's message, type, and code.
//   - [StreamError] is an error frame in a streamed reply, the endpoint's failure after the
//     stream began.
package format
