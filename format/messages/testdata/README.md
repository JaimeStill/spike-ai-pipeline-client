# messages testdata

Anthropic Messages replies recorded from the llama.cpp router on gb10-2 (b11669, set A), each
unstreamed (`.json`), sent with `temperature` 0 and `seed` 7 to `/v1/messages?autoload=false`.
The decode tests compare what `DecodeResponse` gives for each with the response it should be.

- `text.json`: gpt-oss-120b with `system` "Answer with the names alone.", asked "Name three
  French cities, comma-separated, nothing else." with `max_tokens` 256; a thinking block, its
  signature empty, then a text block, stop_reason `end_turn`, and usage with
  `cache_read_input_tokens`.
- `length.json`: Gemma 4 E4B asked "Count from 1 to 20, separated by spaces." with `max_tokens` 8;
  a text block, stop_reason `max_tokens`.
- `conversation.json`: Gemma 4 E4B with `system` as one text block, "Answer in one word.", and the
  messages a system message of "Use capital letters only.", the user's "What is the capital of
  France?", the assistant's "PARIS", and the user's "And of Italy?", with `max_tokens` 64; a
  text block, "ROME", stop_reason `end_turn`, showing the router takes a system message where it
  stands.
- `model_not_found.json`: the router's 400 body for a request naming
  `spike-ai-pipeline/no-such-model`, an OpenAI-style error, `{"error":{"code","message","type"}}`.

Anthropic's own error body, `{"type":"error","error":{"type","message"}}`, isn't recorded: the
tests write it inline, in the shape the Messages API documents.
