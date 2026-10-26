# Changelog

All notable changes to `github.com/JaimeStill/spike-ai-pipeline-client` are documented here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The CI pair: a check workflow on every pull request into and push to main, and a release
  workflow that publishes a pushed `v*` tag's section of this changelog.
- Text chat, in four packages importing one way. `protocol` is the model: a `Request` of Open
  Responses message items (spec 2026-04-24, stateless subset) and a `Response` with output
  items, status, incomplete details, and usage, with `Request.Validate` for the spec's rules. A
  request's `Native` raw-JSON object, the native tier, is merged into the wire body's top level,
  and a member that clashes with a typed field is refused. `format` holds the `Format` contract.
  The module root, `client`, is the transport: `client.New` builds a `Client` from a validated
  `Config` (base URL, query, token source, the caller's `http.Client`, and a required
  `format.Format`), `Client.Generate` validates and sends a `protocol.Request`, and a non-2xx
  reply is an `*APIError`.
- `format/completions`, the Chat Completions format (`/chat/completions`): instructions and
  message items encoded as messages, `max_output_tokens` sent as `max_completion_tokens`, and
  the reply's message, `finish_reason`, and usage decoded into the response.
- Image input: `protocol.InputImage`, the spec's `input_image` (an image URL, remote or data, and
  a detail level, `ImageDetail`), on user messages as the spec allows, and `protocol.DataURL` to
  build a base64 data URL from bytes and a media type. Chat Completions sends it as an
  `image_url` part.
- Audio input, the first client extension: `protocol.InputAudio` holds a clip's bytes and its
  `AudioFormat`, wav or mp3, on user messages. Its Open Responses wire type is
  `spike-ai-pipeline:input_audio`, extending spec 2026-04-24 until the spec adds an audio input
  part; the protocol package's documentation lists it. Chat Completions sends it as an
  `input_audio` part, the clip in base64.
- Function tools: `Request.Tools` of `protocol.FunctionTool` (name, description, JSON Schema
  parameters, strict), and `Request.ToolChoice`, a closed `protocol.ToolChoice` of a
  `ToolChoiceMode` (auto, none, required), a `FunctionToolChoice` naming one function, or an
  `AllowedToolChoice` narrowing the tools to a set with a mode; nil leaves the choice to the
  endpoint. The items `protocol.FunctionCall` (call id, name, JSON arguments) and
  `protocol.FunctionCallOutput` (call id, and a string or input_text parts) carry calls and their
  results, and `Request.Validate` holds them to the spec's rules. Chat Completions sends tools
  and `tool_choice` in its own forms, folds an assistant message and the calls after it into one
  assistant message with `tool_calls`, sends a call's output as a `tool` message, and decodes a
  reply's `tool_calls` into function_call items after its message. An `allowed_tools` choice of
  mode none has no Chat Completions form and is refused.
- Reasoning: the item `protocol.Reasoning` (id, `ReasoningText` content, `SummaryText` summary,
  and `EncryptedContent`, opaque and owned by the format that wrote it), and
  `Request.Reasoning`, a `ReasoningParam` whose `ReasoningEffort` is the spec's none, low,
  medium, high, or xhigh. A response's output goes back as input as it came: content on an input
  reasoning item is accepted and never sent, as the spec's input reasoning item has none. Chat
  Completions decodes a reply's `reasoning_content` into a reasoning item ahead of the message
  and calls, with the text as content and in a versioned envelope of its own in
  `encrypted_content`; it sends that text back as the `reasoning_content` of the assistant
  message the reasoning led to, and drops a reasoning item whose `encrypted_content` isn't its
  envelope. The effort goes out as `reasoning_effort`.
- Structured output: `Request.Text`, a `protocol.TextParam` of a `Format` and a `Verbosity`
  (low, medium, high). `Format` is a closed `protocol.TextFormat`, the spec's request formats: a
  `PlainTextFormat` or a `JSONSchemaFormat` (name, description, JSON Schema, strict); nil leaves
  it to the endpoint. `Request.Validate` requires a json_schema format's name, in the spec's
  pattern, and a schema that is a JSON object. The response's output_text is the JSON the model
  produced, which the caller decodes. Chat Completions sends the format as `response_format`
  (`{"type":"text"}`, or `{"type":"json_schema","json_schema":{...}}`) and the verbosity as
  `verbosity`.
- Embeddings, beside the protocol and outside the `Format`, on the OpenAI-compatible shape:
  `Client.Embed` sends an `EmbeddingRequest` (model, one or more inputs, and a `Native` raw-JSON
  object, the native tier) to the `embeddings` route and returns `Embeddings`, one `[]float32`
  per input in input order, placed by each reply item's index, and `EmbeddingUsage` (prompt and
  total tokens). The vectors travel as base64 of little-endian float32; a reply that doesn't
  give exactly one whole-float32 vector per input is an error. `format.DecodeOpenAIError`
  decodes the OpenAI-style error body, now shared by Chat Completions and embeddings.

[Unreleased]: https://github.com/JaimeStill/spike-ai-pipeline-client/commits/main
