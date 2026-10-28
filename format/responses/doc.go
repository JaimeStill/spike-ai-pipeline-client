// Package responses is the Open Responses wire format: the adapter that encodes the protocol's
// request model as a /responses request and decodes the reply into its response model. It
// speaks spec 2026-04-24 (https://www.openresponses.org), in its stateless subset, under the
// spec's names, as the protocol package models it, and it targets the endpoints that serve it,
// such as llama.cpp's router. A caller builds it with New at the composition root, as
// client.Config's Format.
//
// Every request is stateless: it carries store false and include
// ["reasoning.encrypted_content"], and never previous_response_id, so the conversation goes out
// whole on each request and the reasoning comes back in a form the next one can send. A Native
// previous_response_id or background member is refused, never sent: the client's model is
// stateless, a request's Input being the whole conversation, and each member asks the endpoint
// to keep state, a prior response or a running one, that the model has no place for. Stateful
// use would be a new capability of the client, typed fields with operations to retrieve, poll,
// cancel, and delete a stored response, never a member a request carries.
//
// Reasoning goes back through a reasoning item's encrypted_content, where DecodeResponse writes
// the format's envelope, spike-ai-pipeline:responses/v1: and base64 of
// {"encrypted_content","content"}: the server's encrypted_content, and the texts of the item's
// reasoning_text parts. In the spec's form, the zero Options, EncodeRequest sends the server's
// encrypted_content back, and never content, dropping an item whose server encrypted_content is
// empty. Options.ResendReasoningText sends the texts back as content too: the one exception to
// protocol.Reasoning's rule that a format never sends a reasoning item's text on input, for an
// endpoint such as llama.cpp's router, which gives no encrypted_content and reads the reasoning
// from content. Another format's envelope is dropped on encode.
//
// Audio input goes out as the protocol's client extension, spike-ai-pipeline:input_audio, the
// spec having no audio input part.
//
// The format's Options adjust it to an endpoint that departs from the spec; their zero value is
// the spec's form.
//
// llama.cpp's router, b11669, rebuilds a /responses body as a Chat Completions one: it reads
// instructions as a system message, max_output_tokens as max_tokens, reasoning's effort as
// reasoning_effort, the tools, and the input's items, and refuses a content part other than
// input_text, input_image, and input_file, the audio extension among them. It joins an item
// that follows an assistant turn, a message, a function_call, or a reasoning item, to that
// turn, so a reasoning item goes out ahead of the message or call it led to; it reads a
// reasoning item's first content part alone, and needs one, which is why the router takes
// Options.ResendReasoningText; and a reasoning item that follows an assistant turn replaces the
// turn's reasoning. It drops what Chat Completions doesn't read: text, under its own name, so
// the text format and verbosity reach no model; reasoning's summary; an image's detail; store
// and include; and a named function or allowed_tools tool choice, which it treats as auto. A
// tool's strict flag reaches it, and defaults to true there, which is why strict is always
// sent. A Native member it doesn't rebuild passes through to Chat Completions, which reads
// members such as temperature, top_p, seed, and response_format. Its reply always has status
// completed, even when the output stopped at max_output_tokens.
//
// This version of the format sends and decodes an unstreamed request; a streamed reply isn't
// decoded.
//
// Its exported identifiers:
//
//   - [Format] is the format, a format.Format.
//   - [Options] adjusts the format to an endpoint that departs from the spec.
//   - [New] returns the format.
//   - [Format.Route] is the route it posts to, responses.
//   - [Format.EncodeRequest] encodes a request as a create-response body: model, instructions,
//     input items, tools, tool choice, reasoning, text, max_output_tokens, and the native tier.
//   - [Format.DecodeResponse] decodes a response resource into a response: messages,
//     reasoning, and function calls, with their ids and statuses the server's.
//   - [Format.EncodeStreamRequest] encodes a request as EncodeRequest does, with stream true.
//   - [Format.NewStreamDecoder] returns a decoder that refuses the stream, as this version
//     decodes none.
//   - [Format.DecodeError] decodes an OpenAI-style error body.
package responses
