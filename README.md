# spike-ai-pipeline-client

The model-client layer of [spike-ai-pipeline](https://github.com/JaimeStill/spike-ai-pipeline):
Go primitives over the inference engines' native APIs, built under go-elemental as owned,
optimized infrastructure. One standard-library module holds a stable request and response
model; each wire format (OpenAI Chat Completions first, then Anthropic Messages) is a package
behind one Format interface, chosen by typed constructor at the composition root.

It covers chat with text, image, and audio parts, tools, structured output, and reasoning;
embeddings; and transcription, streamed as typed deltas. It is released by tag, `vX.Y.Z`, with a
CHANGELOG, and spike-ai-pipeline-processing and the root build on it by tag.

The spike's question, evidence, and answer are in the root repository's README.

Apache-2.0.
