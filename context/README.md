# spike-ai-pipeline-client

The model-client layer of spike-ai-pipeline, released by tag and built on by
spike-ai-pipeline-processing and the root. This context records only working knowledge the code
and the README do not express; the spike's question and evidence live in the root repository's
`context/README.md`.

## Capability map

- **model**: the request and response model shared by every format.
- **formats**: one package per wire format behind the Format interface: Chat Completions, then
  Anthropic Messages.
- **streaming**: the SSE decoder and the typed deltas with their accumulator.
- **retry**: the opt-in retry policy.
