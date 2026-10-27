# Changelog

All notable changes to `github.com/JaimeStill/spike-ai-pipeline-client` are documented here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.2.0] - 2026-10-27

### Added

- `Client.GenerateStream`, which streams a `protocol.Request`'s response as Open Responses'
  streaming events (spec 2026-04-24) over server-sent events, decoded to the WHATWG standard on
  the standard library alone. It returns a lazy `iter.Seq2[protocol.Event, error]`: nothing is
  validated, encoded, or sent until it is ranged, each range sends a new request, the events are
  read as they arrive inside the range loop with no goroutine, and breaking out of the loop
  closes the body. A refused request, a token source error, a transport failure, or a non-2xx
  reply (the same `*APIError` as `Generate`) is the sequence's only pair. A stream that breaks
  ends with one error pair: a cancelled or expired context's error, wrapped, or
  `io.ErrUnexpectedEOF` for a stream that ends before its terminal event or a read that fails.
  Cancelling closes the connection, which stops the generation.
- `protocol.Event`, a closed set of the spec's streaming events under the spec's names, each with
  its `sequence_number` and, where the spec gives them, `output_index`, `content_index`, and
  `item_id`: `ResponseCreated` and the terminal `ResponseCompleted`, `ResponseIncomplete`, and
  `ResponseFailed`; `ErrorEvent`, the error event, which `response.failed` follows, carrying an
  `ErrorPayload` (type, code, message, and param); `OutputItemAdded` and `OutputItemDone`;
  `ContentPartAdded` and `ContentPartDone`; `OutputTextDelta` and `OutputTextDone`,
  `RefusalDelta` and `RefusalDone`, and `ReasoningDelta` and `ReasoningDone`; and
  `FunctionCallArgumentsDelta` and `FunctionCallArgumentsDone`. Reasoning summary events,
  output_text annotations, logprobs, and the deltas' obfuscation padding are outside the subset.
- `protocol.Accumulator`, which folds a stream's events in the caller's own loop: `Add` takes
  each event and refuses one that doesn't follow the events before it, changing nothing, and
  `Response` returns the Response folded so far. After the terminal event it is that event's
  Response, the Response `Generate` decodes for the same reply; after a broken stream it holds
  the partial output, in progress.
- `protocol.StatusInProgress` and `protocol.StatusFailed`, the spec's in_progress and failed.
- `Response.Error`, a `*protocol.ResponseError` (code and message), the spec's error of a failed
  response. A failure the server reports mid-stream, such as llama.cpp's error frame, ends the
  stream with an `ErrorEvent` and then `ResponseFailed`, whose Response is failed, holds the
  error, and holds the output made before it.
- `format.StreamDecoder`, which turns one streamed reply's events into the protocol's, a new one
  per stream so a Format stays safe for concurrent use, and the `format.Format` methods
  `EncodeStreamRequest` and `NewStreamDecoder`.
- Streaming in `format/completions`: the streamed request sends `stream` true and
  `stream_options.include_usage` true, so the terminal Response carries usage. Its stream
  decoder turns chunks into the spec's events in v0.1.0's output order (reasoning, message,
  function calls), each item opened by `output_item.added`, its parts by `content_part.added`,
  closed by their deltas' done events, `content_part.done`, and `output_item.done` with the
  whole item, the reasoning item's carrying the same `encrypted_content` envelope
  `DecodeResponse` makes, so the folded output goes back as input as it came. Tool-call
  fragments are assembled by their index. `data: [DONE]` ends the stream, as does the stream's
  end after a `finish_reason`; ping comments are ignored.
- `Config.Retry`, a `*RetryPolicy` (`MaxAttempts`, `BaseDelay`, `MaxDelay`), opt-in: nil, the
  default, retries nothing, and `New` refuses fewer than one attempt, a negative delay, or a base
  delay above the most delay. With a policy, `Generate`, `Embed`, and `GenerateStream` before
  its 2xx reply arrives send a request again on a 408, 429, or 503 reply, or a connection
  failure before any reply, calling the token source again and sending the same body, after an
  exponential backoff with full jitter capped at `MaxDelay`. A `Retry-After`, in seconds or as
  an HTTP date, replaces the backoff; one longer than `MaxDelay`, or a wait past the context's
  deadline, returns the last error at once, and a context cancelled while waiting returns its
  error. Never retried: a 500 or any other status, a context error, a token source error, a
  stream once started, and `Transcribe`, whose clip is a one-shot reader.

### Changed

- **Breaking:** `format.Format` gains the required methods `EncodeStreamRequest` and
  `NewStreamDecoder`. A format written outside the module must add them.
- `format/completions` derives each output item's id from the reply's id and its output index,
  `<reply id>/<output index>`, such as `chatcmpl-abc/1`; v0.1.0 left `Message.ID`,
  `FunctionCall.ID`, and `Reasoning.ID` empty. A reply with no id still gives its items none.
- Per-item status follows the spec: only the last output item takes the response's status, and
  the items before it are completed. v0.1.0 gave every message and function call the
  response's status, so a reply cut off by length or a content filter had every item
  incomplete.
- `protocol.Reasoning` gains `Status`, as the spec's required item fields have every item carry
  one, ignored on input as `Message`'s and `FunctionCall`'s are; `DecodeResponse` sets it where
  v0.1.0 left it empty.
- The Chat Completions request always sends `stream` as a typed field, false unstreamed and true
  streamed, so a `Native` `stream` or `stream_options` member is refused as a clash before
  anything is sent. v0.1.0 sent a `Native` `"stream": true` and then couldn't decode the reply.
- `protocol.ReasoningText` is a `ContentPart`, as the spec's content part events carry it;
  `Request.Validate` still refuses it in a message.
- `Config.HTTP`'s documentation names the timeouts to use with a stream: the context, or the
  transport's `ResponseHeaderTimeout`, never `http.Client.Timeout`, which ends a long stream part
  way.

## [v0.1.0] - 2026-10-27

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

[Unreleased]: https://github.com/JaimeStill/spike-ai-pipeline-client/compare/v0.2.0...HEAD
[v0.2.0]: https://github.com/JaimeStill/spike-ai-pipeline-client/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/JaimeStill/spike-ai-pipeline-client/releases/tag/v0.1.0
