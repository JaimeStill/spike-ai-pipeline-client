# spike-ai-pipeline-client

The model-client layer of spike-ai-pipeline, released by tag and built on by
spike-ai-pipeline-processing and the root. This context records only working knowledge the code
and the README do not express; the spike's question and evidence live in the root repository's
`context/README.md`.

## Capability map

- **protocol**: the request and response model (`protocol`).
- **formats**: the `format` contract and a package per wire format below it: Chat Completions
  (`format/completions`); Anthropic Messages and Open Responses are planned.
- **transport**: the module root, package `client`: `Generate`, `Embed`, and `Transcribe`.
- **streaming**: planned: `GenerateStream`, over an in-house SSE decoder.
- **retry**: planned: an opt-in retry policy.
