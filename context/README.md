# spike-ai-pipeline-client

The model-client layer of spike-ai-pipeline, released by tag and built on by
spike-ai-pipeline-processing and the root. This context records only working knowledge the code
and the README do not express; the spike's question and evidence live in the root repository's
`context/README.md`.

## Capability map

- **protocol**: the request and response model (`protocol`).
- **formats**: the `format` contract and a package per wire format below it: Chat Completions
  (`format/completions`); Anthropic Messages and Open Responses are planned.
- **transport**: the module root, package `client`: `Generate`, `GenerateStream`, `Embed`, and
  `Transcribe`.
- **streaming**: `GenerateStream` yields Open Responses' streaming events, decoded by the
  internal SSE decoder (`internal/sse`), and `protocol.Accumulator` folds them into a Response.
  The integration tier proves it on gpt-oss-120b on gb10-2: streamed text, a function call, the
  reasoning round trip, and a cancel mid-stream.
- **retry**: the opt-in `Config.Retry` policy, proven by unit tests alone, since the router can't
  answer 408, 429, or 503 on demand. It doesn't match net/http's unexported error for a server
  closing an idle connection, so that error isn't retried.
