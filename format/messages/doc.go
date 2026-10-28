// Package messages is the Anthropic Messages wire format: the adapter that encodes the protocol's
// request model as a /messages request and decodes the reply into its response model. It speaks
// API version 2026-10-21, which every request names in its anthropic-version header, and it
// targets Anthropic's API and the endpoints compatible with it, such as llama.cpp's router. A
// caller builds it with New at the composition root, as client.Config's Format.
//
// A streamed reply's content blocks may be open at once, as llama.cpp's router stops every block
// only at the end of the reply; the decoder closes each part at its own block's stop. An error
// frame in the stream, Anthropic's {"type":"error",...} or llama.cpp's {"code","message","type"},
// ends it with the error event and response.failed.
//
// Reasoning goes back through a reasoning item's encrypted_content, where DecodeResponse writes
// the format's envelope, spike-ai-pipeline:messages/v1: and base64 of the thinking block's
// {"thinking","signature"} or the redacted_thinking block's {"data"}; another format's envelope
// is dropped on encode.
//
// The hints Messages has no form for are dropped, never refused: an image's detail; reasoning's
// summary; text's verbosity; and a JSON-schema text format's name, description, and strict
// flag. Only input_audio, which has no Messages form either, is refused.
//
// llama.cpp's router, b11669, rebuilds a Messages body as a Chat Completions one and keeps only
// the members it reads: messages, tools, tool_choice, stop_sequences, max_tokens, temperature,
// top_p, top_k, stream, chat_template_kwargs, thinking, and metadata. It drops output_config, so
// reasoning effort's level and structured output's schema reach no model, and a tool's strict
// flag; it treats tool_choice none as no choice, the tools still live, and a named tool as any.
// A Native member reaches its model only if the router reads it, as temperature, top_p, top_k,
// and chat_template_kwargs are; Anthropic's API reads every member it defines.
//
// Its exported identifiers:
//
//   - [Format] is the format, a format.Format.
//   - [New] returns the format.
//   - [Format.Route] is the route it posts to, messages.
//   - [Format.EncodeRequest] encodes a request as a Messages body: messages, tools, tool choice,
//     reasoning effort, text format, and the native tier.
//   - [Format.DecodeResponse] decodes a Messages reply into a response: reasoning, messages, and
//     function calls.
//   - [Format.EncodeStreamRequest] encodes a request as EncodeRequest does, with stream true.
//   - [Format.NewStreamDecoder] returns a decoder of one streamed reply's events into the
//     protocol's streaming events, whose folded Response is DecodeResponse's for the same reply.
//   - [Format.DecodeError] decodes an Anthropic or an OpenAI-style error body.
package messages
