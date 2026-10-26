// Package format is the contract a wire format meets: [Format] converts the protocol package's
// request and response model to and from one API's wire shape. It imports only protocol; each
// implementation is a package below it, named for the endpoint it targets, such as completions
// for Chat Completions, and none imports the client.
//
// It holds the contract and what the formats share: the OpenAI-style error body, which every
// OpenAI-compatible endpoint answers with, so completions decodes its errors with it, and so
// does the client for its operations outside the Format, such as embeddings.
//
// Its exported identifiers:
//
//   - [Format] is a wire format, the adapter between the protocol and one API's shape.
//   - [DecodeOpenAIError] decodes an OpenAI-style error body's message, type, and code.
package format
