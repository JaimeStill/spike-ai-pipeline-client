// Package messages is the Anthropic Messages wire format: the adapter that encodes the protocol's
// request model as a /messages request and decodes the reply into its response model. It speaks
// API version 2026-10-21, which every request names in its anthropic-version header, and it
// targets Anthropic's API and the endpoints compatible with it, such as llama.cpp's router. A
// caller builds it with New at the composition root, as client.Config's Format.
//
// Its exported identifiers:
//
//   - [Format] is the format, a format.Format.
//   - [New] returns the format.
//   - [Format.Route] is the route it posts to, messages.
//   - [Format.EncodeRequest] encodes a request as a Messages body.
//   - [Format.DecodeResponse] decodes a Messages reply into a response.
//   - [Format.EncodeStreamRequest] refuses every request: the format doesn't stream yet.
//   - [Format.NewStreamDecoder] returns a decoder that refuses every event, for the same reason.
//   - [Format.DecodeError] decodes an Anthropic or an OpenAI-style error body.
package messages
