// Package completions is the Chat Completions wire format: the adapter that encodes the
// protocol's request model as an OpenAI-compatible /chat/completions request and decodes the
// reply into its response model, whole or streamed. It targets /chat/completions, not the legacy
// /completions API. A caller builds it with New at the composition root, as client.Config's
// Format.
//
// Its exported identifiers:
//
//   - [Format] is the format, a format.Format.
//   - [New] returns the format.
//   - [Format.Route] is the route it posts to, chat/completions.
//   - [Format.EncodeRequest] encodes a request as a Chat Completions body.
//   - [Format.DecodeResponse] decodes a Chat Completions reply into a response.
//   - [Format.EncodeStreamRequest] encodes a request as a streamed Chat Completions body.
//   - [Format.NewStreamDecoder] returns a decoder turning one streamed reply's chunks into the
//     protocol's streaming events.
//   - [Format.DecodeError] decodes an OpenAI-style error body.
package completions
