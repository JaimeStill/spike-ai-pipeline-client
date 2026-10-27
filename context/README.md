# spike-ai-pipeline-client

The model-client layer of spike-ai-pipeline, released by tag and built on by
spike-ai-pipeline-processing and the root. This context records only working knowledge the code
and the README do not express; the spike's question and evidence live in the root repository's
`context/README.md`.

## Capability map

- **protocol**: the request and response model (`protocol`).
- **formats**: the `format` contract and a package per wire format below it: Chat Completions
  (`format/completions`) and Anthropic Messages (`format/messages`, API version 2026-10-21);
  Open Responses is planned. Messages carries reasoning back through its own
  `encrypted_content` envelope, `spike-ai-pipeline:messages/v1:`, and drops another format's.
  Its testdata is recorded from the router on gb10-2 (b11669), whose Messages route rebuilds the
  body as Chat Completions and drops `output_config`, so effort and structured output's schema
  reach no model through it.
- **transport**: the module root, package `client`: `Generate`, `GenerateStream`, `Embed`, and
  `Transcribe`.
- **streaming**: `GenerateStream` yields Open Responses' streaming events, decoded by the
  internal SSE decoder (`internal/sse`), and `protocol.Accumulator` folds them into a Response.
  The integration tier proves it on gpt-oss-120b on gb10-2, through both formats: streamed text,
  a function call, the reasoning round trip, and a cancel mid-stream.
- **integration through both formats**: every `Generate` and `GenerateStream` scenario runs
  through Chat Completions and Messages from one piece of test code (`eachFormat`), with
  `MaxOutputTokens` set, as Messages requires it; audio input stays Chat Completions only. A hint
  the router drops is recorded rather than asserted, one `recorded:` line per outcome in a verbose
  run's log: on Messages, structured output; on both, reasoning effort and the named,
  allowed_tools, and none tool choices, as llama.cpp b11669 treats the named and allowed_tools
  choices as auto on Chat Completions.
- **retry**: the opt-in `Config.Retry` policy, proven by unit tests alone, since the router can't
  answer 408, 429, or 503 on demand. It doesn't match net/http's unexported error for a server
  closing an idle connection, so that error isn't retried.
