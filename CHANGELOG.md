# Changelog

All notable changes to `github.com/JaimeStill/spike-ai-pipeline-client` are documented here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.4.0] - 2026-10-28

### Added

- `format/responses`, the Open Responses format (`/responses`, spec 2026-04-24), built with
  `responses.New(responses.Options{})` as `client.Config`'s `Format`. It sends the protocol's
  request under the spec's own names, so every hint the protocol carries goes out and none is
  dropped: instructions, input items in order, tools with `strict` always sent, the tool choice,
  reasoning's effort and summary, the text format and verbosity, and `max_output_tokens`. The
  native tier merges into the body's top level as in the other formats, a clashing member
  refused. It targets the endpoints that serve the spec, such as llama.cpp's router.
- Every Open Responses request is stateless: it sends `store` false and `include`
  `["reasoning.encrypted_content"]`, never `previous_response_id`, and a `Native`
  `previous_response_id` or `background` member is refused before anything is sent, as each asks
  the endpoint to keep state that the client's model, a request's input being the whole
  conversation, has no place for. A reply in progress or queued is an error.
- Reasoning through Open Responses: `DecodeResponse` writes the format's envelope into a
  reasoning item's `encrypted_content`, `spike-ai-pipeline:responses/v1:` and base64 of
  `{"encrypted_content","content"}`, the server's `encrypted_content` and the item's reasoning
  texts. In the spec's form, the zero `Options`, the item goes back by the server's
  `encrypted_content` alone, never its text, and is dropped when the server gave none. Another
  format's envelope is dropped on encode, never an error.
- `responses.Options` and its one option, `ResendReasoningText`, which sends a reasoning item's
  texts back as its content too, for an endpoint such as llama.cpp's router that gives no
  `encrypted_content` and reads the reasoning from there; a run of reasoning that no assistant
  message or function_call follows is dropped, as the router refuses one. It is the one
  exception to `protocol.Reasoning`'s rule that a format never sends reasoning content on
  input, and even then the text sent is the envelope's, never the item's `Content`.
- Audio input through Open Responses as the client extension's own wire form,
  `{"type":"spike-ai-pipeline:input_audio","data","format"}`, its data the clip in standard
  base64 and its format wav or mp3.
- `DecodeResponse` decodes a response resource's messages, reasoning items, and function calls
  with the server's ids and statuses, its status, `incomplete_details`, a failed response's
  error, and its usage; `DecodeError` decodes the OpenAI-style error body with
  `format.DecodeOpenAIError`, the spec defining none.
- Streaming in `format/responses`: `EncodeStreamRequest` sends `stream` true, and the stream
  decoder passes the spec's semantic events through, numbered afresh, and normalizes a stream
  that departs from the spec, as llama.cpp's router's does: an event with no `sequence_number`,
  `output_index`, or `content_index` is placed by arrival order and its `item_id`;
  `response.reasoning_text.delta` and `.done`, OpenAI's names, are read as the spec's
  `response.reasoning.delta` and `.done`; the done and part events the stream leaves out are
  filled in, never repeating one the server sent; and an item of no status at its
  `output_item.done` is completed. The folded Response is `DecodeResponse`'s of the terminal
  response, except that an item the router gives no status there, its streamed reasoning item,
  takes the completed status of its `output_item.done`. An error frame, the spec's error event
  or llama.cpp's `{"error":{...}}` of no type, ends the stream with the error event and
  `response.failed`.
- `protocol.ReasoningParam.Summary`, a `protocol.ReasoningSummary` of `ReasoningSummaryAuto`,
  `ReasoningSummaryConcise`, or `ReasoningSummaryDetailed`, which asks for a summary of the
  model's reasoning; `Request.Validate` refuses any other value. Open Responses sends it as
  `reasoning.summary`.
- `protocol.MinMaxOutputTokens`, 16, the spec's least `max_output_tokens`.

### Changed

- **Breaking:** a behavior change: `Request.Validate`, and so every operation that sends a
  `protocol.Request`, refuses a `MaxOutputTokens` of 1 through 15, below `MinMaxOutputTokens`,
  whatever the format. v0.3.0 sent it. Zero still leaves the bound to the endpoint.
- `format/completions` and `format/messages` drop reasoning's summary, which their APIs have no
  form for, never refusing it.

## [v0.3.0] - 2026-10-27

### Added

- `format/messages`, the Anthropic Messages format (`/messages`), built with `messages.New()` as
  `client.Config`'s `Format`. It speaks API version 2026-10-21, which every request names in its
  `anthropic-version` header, and targets Anthropic's API and the endpoints compatible with it,
  such as llama.cpp's router. It sends `MaxOutputTokens` as `max_tokens`, which Messages
  requires, so a request with no `MaxOutputTokens` is refused; instructions as the top-level
  `system`; a system or developer message as a system message where it stands; an assistant
  run as one assistant message of thinking, text, and `tool_use` blocks; a function call's
  output as a `tool_result` block leading its user message; images as image blocks of a base64
  or url source; tools as custom tools with `input_schema`; the tool choice as `tool_choice`,
  an `allowed_tools` choice sending only the tools it allows; reasoning effort none as thinking
  disabled and any other as adaptive thinking with `output_config.effort`; and a JSON-schema
  text format as `output_config.format`. The native tier merges into the body's top level as
  in Chat Completions, a clashing member refused. Audio input has no Messages form and is
  refused.
- The hints Messages has no form for are dropped, never refused: an image's detail, text's
  verbosity, and a JSON-schema format's name, description, and strict flag.
- Reasoning through Messages: `DecodeResponse` makes a reasoning item of each thinking or
  redacted_thinking block, its `encrypted_content` the format's envelope,
  `spike-ai-pipeline:messages/v1:` and base64 of the block's `{"thinking","signature"}` or
  `{"data"}`, which goes back as the block it came from. A reasoning item whose
  `encrypted_content` isn't this envelope, such as Chat Completions', is dropped on encode,
  never an error, so a conversation can carry another format's reasoning through this one.
- `DecodeResponse` decodes a Messages reply's text, `tool_use`, thinking, and
  redacted_thinking blocks into reasoning, messages, and function calls, its `stop_reason` into
  the status and `IncompleteDetails`, `pause_turn` an error as a stateless response has no place
  for it, and its usage, the prompt cache's reads and writes counted in the input tokens.
  `DecodeError` decodes Anthropic's error body and llama.cpp's OpenAI-style one, both with
  `format.DecodeOpenAIError`.
- Streaming in `format/messages`: `EncodeStreamRequest` sends `stream` true, and the stream
  decoder turns a Messages stream into the spec's events, whose folded Response is
  `DecodeResponse`'s for the same reply. It takes content blocks open at once, as llama.cpp's
  router stops every block only at the end of the reply, and an error frame in Anthropic's
  shape or llama.cpp's ends the stream with the error event and `response.failed`.

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

[Unreleased]: https://github.com/JaimeStill/spike-ai-pipeline-client/compare/v0.4.0...HEAD
[v0.4.0]: https://github.com/JaimeStill/spike-ai-pipeline-client/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/JaimeStill/spike-ai-pipeline-client/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/JaimeStill/spike-ai-pipeline-client/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/JaimeStill/spike-ai-pipeline-client/releases/tag/v0.1.0
