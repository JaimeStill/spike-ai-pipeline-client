# Changelog

All notable changes to `github.com/JaimeStill/spike-ai-pipeline-client` are documented here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.1.0] - 2026-10-26

### Added

- The model client, on the standard library alone, in four layers importing one way: `protocol`,
  the request and response model; `format`, the contract a wire format meets; a package per wire
  format below it; and the module root, `client`, the transport.
- `protocol`, Open Responses' item model (spec 2026-04-24) in its stateless subset, under the
  spec's names. A `Request` carries the model, instructions, input items, tools, tool choice,
  reasoning, and text configuration; `Request.Validate` refuses one the spec's rules don't
  allow. A `Response` carries the output items, status, `IncompleteDetails`, and `Usage`. A
  request's `Native` raw-JSON object, the native tier, is merged into the wire body's top level,
  and a member that clashes with a typed field is refused when the format encodes the request,
  before anything is sent.
- The `Message` item, content parts from a `Role` (user, system, developer, or assistant):
  `InputText`, `InputImage` (an image URL, remote or data, with an `ImageDetail`; `DataURL`
  builds a base64 data URL from bytes and a media type), and on output `OutputText` and
  `Refusal`.
- Audio input, the first client extension: `InputAudio`, a clip's bytes and its `AudioFormat`
  (wav or mp3), on user messages, with the wire type `spike-ai-pipeline:input_audio` until the
  spec adds an audio input part. The `protocol` package documentation lists the client
  extensions.
- Function tools: `FunctionTool` (name, description, JSON Schema parameters, strict) and a
  closed `ToolChoice` of a `ToolChoiceMode` (auto, none, required), a `FunctionToolChoice`, or an
  `AllowedToolChoice`; the items `FunctionCall` and `FunctionCallOutput` carry calls and their
  results.
- Reasoning: the `Reasoning` item (`ReasoningText` content, `SummaryText` summary, and
  `EncryptedContent`, opaque and owned by the format that wrote it), and a `ReasoningParam`
  whose `ReasoningEffort` is none, low, medium, high, or xhigh. A response's output goes back as
  input as it came.
- Structured output: a `TextParam` of a closed `TextFormat`, `PlainTextFormat` or
  `JSONSchemaFormat` (name, description, JSON Schema, strict), and a `Verbosity` (low, medium,
  high). The response's output_text is the JSON the model produced, which the caller decodes.
- `format.Format`, the contract, and `format.DecodeOpenAIError`, which decodes the OpenAI-style
  error body every OpenAI-compatible endpoint answers with.
- `format/completions`, the Chat Completions format (`/chat/completions`). It sends
  `max_output_tokens` as `max_completion_tokens`, image and audio input as `image_url` and
  `input_audio` parts, tools and `tool_choice` in its own forms, a function call's output as a
  `tool` message, reasoning effort as `reasoning_effort`, the text format as `response_format`,
  and verbosity as `verbosity`. It decodes the reply's message, `tool_calls`, `finish_reason`,
  and usage, and its `reasoning_content` into a reasoning item, which it sends back as the
  `reasoning_content` of the assistant message the reasoning led to. An `allowed_tools` choice
  of mode none has no Chat Completions form and is refused.
- `client.New`, which builds a `Client` from a validated `Config`: base URL, query, a
  `TokenSource` for bearer tokens, the caller's `http.Client`, and a required `format.Format`.
  The library sets no default model, timeout, retry, or size limit. A non-2xx reply is an
  `*APIError`.
- `Client.Generate`, which validates a `protocol.Request`, sends it in the Client's format, and
  returns the `protocol.Response`.
- `Client.Embed`, beside the protocol on the OpenAI-compatible `embeddings` route whatever the
  format: an `*EmbeddingRequest` (model, one or more inputs, and a `Native` object) returns
  `Embeddings`, one `[]float32` per input in input order, and `EmbeddingUsage`. The vectors
  travel as base64 of little-endian float32.
- `Client.Transcribe`, beside the protocol on the OpenAI-compatible `audio/transcriptions`
  route: a `*TranscriptionRequest` (model, the clip as an `io.Reader` with its file name, an
  optional language and prompt, and a `Native` `url.Values`, the native tier, sent as extra form
  fields, a field that clashes with a typed one refused) returns a `Transcription`, the text and
  `TranscriptionUsage`. The clip streams as a chunked multipart upload, never held whole in
  memory, and `Transcribe` returns only once it has stopped reading it. The reply carries no
  timestamps.

[Unreleased]: https://github.com/JaimeStill/spike-ai-pipeline-client/compare/v0.1.0...HEAD
[v0.1.0]: https://github.com/JaimeStill/spike-ai-pipeline-client/releases/tag/v0.1.0
