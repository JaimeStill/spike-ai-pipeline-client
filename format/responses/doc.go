// Package responses is the Open Responses wire format: the adapter that encodes the protocol's
// request model as a /responses request and decodes the reply into its response model. It
// speaks spec 2026-04-24 (https://www.openresponses.org), in its stateless subset, under the
// spec's names, as the protocol package models it, and it targets the endpoints that serve it,
// such as llama.cpp's router. A caller builds it with New at the composition root, as
// client.Config's Format.
//
// Every request is stateless: it carries store false and include
// ["reasoning.encrypted_content"], and never previous_response_id, so the conversation goes out
// whole on each request and the reasoning comes back in a form the next one can send.
//
// Audio input goes out as the protocol's client extension, spike-ai-pipeline:input_audio, the
// spec having no audio input part.
//
// The format's Options adjust it to an endpoint that departs from the spec; their zero value is
// the spec's form.
//
// llama.cpp's router, b11669, rebuilds a /responses body as a Chat Completions one: it reads
// instructions as a system message, max_output_tokens as max_tokens, and the input's messages,
// and refuses a content part other than input_text, input_image, and input_file, the audio
// extension among them. It passes text through under its own name, which Chat Completions
// doesn't read, so the text format and verbosity reach no model. Its reply always has status
// completed, even when the output stopped at max_output_tokens.
//
// This version of the format sends and decodes an unstreamed request of messages: tools, a tool
// choice, reasoning, and the input items other than messages are refused, and a streamed reply
// isn't decoded. A reply's reasoning and function_call items are decoded.
//
// Its exported identifiers:
//
//   - [Format] is the format, a format.Format.
//   - [Options] adjusts the format to an endpoint that departs from the spec.
//   - [New] returns the format.
//   - [Format.Route] is the route it posts to, responses.
//   - [Format.EncodeRequest] encodes a request as a create-response body: model, instructions,
//     input messages, text, max_output_tokens, and the native tier.
//   - [Format.DecodeResponse] decodes a response resource into a response: messages,
//     reasoning, and function calls, with their ids and statuses the server's.
//   - [Format.EncodeStreamRequest] encodes a request as EncodeRequest does, with stream true.
//   - [Format.NewStreamDecoder] returns a decoder that refuses the stream, as this version
//     decodes none.
//   - [Format.DecodeError] decodes an OpenAI-style error body.
package responses
