# spike-ai-pipeline-client

The model-client layer of spike-ai-pipeline, released by tag and built on by
spike-ai-pipeline-processing and the root. This context records only working knowledge the code
and the README do not express; the spike's question and evidence live in the root repository's
`context/README.md`.

## Capability map

- **protocol**: the request and response model (`protocol`). `ReasoningParam.Summary` (auto,
  concise, detailed) is sent by Open Responses alone, the other formats dropping it.
  `MinMaxOutputTokens`, 16, is the spec's least `max_output_tokens`: since v0.4.0 `Validate`
  refuses 1 through 15 for every format, zero still leaving the bound to the endpoint.
- **formats**: the `format` contract and a package per wire format below it: Chat Completions
  (`format/completions`), Anthropic Messages (`format/messages`, API version 2026-10-21), and
  Open Responses (`format/responses`, spec 2026-04-24). Messages carries reasoning back through
  its own `encrypted_content` envelope, `spike-ai-pipeline:messages/v1:`, and drops another
  format's. Its testdata is recorded from the router on gb10-2 (b11669), whose Messages route
  rebuilds the body as Chat Completions and drops `output_config`, so effort and structured
  output's schema reach no model through it.
- **Open Responses**: `responses.New(Options)`, stateless (`store` false, `include`
  `reasoning.encrypted_content`, a `Native` `previous_response_id` or `background` refused). Its
  envelope, `spike-ai-pipeline:responses/v1:`, holds the server's `encrypted_content` and the
  reasoning texts; `Options.ResendReasoningText` resends the texts as content, the one exception
  to "a format never sends reasoning content", which the router needs, as it gives no
  `encrypted_content` and reads a reasoning item's first content part. Audio goes out as
  `{"type":"spike-ai-pipeline:input_audio","data","format"}`, which the router refuses. The
  router's stream departs from the spec and the decoder normalizes it: no sequence numbers or
  indexes, `response.reasoning_text.*` read as `response.reasoning.*`, missing done events filled
  in, and a reasoning item of no status completed. Its testdata is recorded from the router on
  gb10-2 (b11669), which drops `text` and reasoning's summary and reports every reply completed,
  even at the token bound.
- **transport**: the module root, package `client`: `Generate`, `GenerateStream`, `Embed`, and
  `Transcribe`.
- **streaming**: `GenerateStream` yields Open Responses' streaming events, decoded by the
  internal SSE decoder (`internal/sse`), and `protocol.Accumulator` folds them into a Response.
  The integration tier proves it on gpt-oss-120b on gb10-2, through every format: streamed text,
  a function call, the reasoning round trip, and a cancel mid-stream.
- **integration through every format**: every `Generate` and `GenerateStream` scenario runs
  through Chat Completions, Messages, and Open Responses (with `ResendReasoningText`) from one
  piece of test code (`eachFormat`), with `MaxOutputTokens` set, as Messages requires it. Audio
  input is asserted on Chat Completions; on Open Responses the router's answer to the extension
  is recorded. A hint the router drops is recorded rather than asserted, one `recorded:` line per
  outcome in a verbose run's log: on Messages and Open Responses, structured output; on Open
  Responses, the status at the token bound; on every format, reasoning effort and the named,
  allowed_tools, and none tool choices, as llama.cpp b11669 treats the named and allowed_tools
  choices as auto on Chat Completions and Open Responses.
- **retry**: the opt-in `Config.Retry` policy, proven by unit tests alone, since the router can't
  answer 408, 429, or 503 on demand. It doesn't match net/http's unexported error for a server
  closing an idle connection, so that error isn't retried.
