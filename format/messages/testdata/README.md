# messages testdata

Anthropic Messages replies recorded from the llama.cpp router on gb10-2 (b11669, set A), each
unstreamed (`.json`), and some streamed too (`.sse`, the body as it arrived) for the same request,
sent with `temperature` 0 and `seed` 7 to `/v1/messages?autoload=false`. The decode tests compare
what `DecodeResponse` gives for each `.json` with the response it should be. The stream tests
translate each `.sse` and fold its events, and compare the result with what `DecodeResponse` gives
for its `.json` given the stream's reply id and tool_use ids, as the two were separate requests,
and with each call's arguments the stream's text, which the `.json`'s input holds as the same JSON
written compact: the router parses a call's arguments for an unstreamed reply, and streams the
model's own text.

Each streamed request sent `"stream":true`, as `EncodeStreamRequest` does, and each unstreamed
one `"stream":false`. The router's prompt cache makes a repeated request's reply differ at times,
so a `.sse` is paired with the `.json` it matches: `text.sse` and `length.sse` with replies
recorded before them, which they match token for token, and `tool_use.sse` with the `.json`
recorded beside it.

- `text.json`: gpt-oss-120b with `system` "Answer with the names alone.", asked "Name three
  French cities, comma-separated, nothing else." with `max_tokens` 256; a thinking block, its
  signature empty, then a text block, stop_reason `end_turn`, and usage with
  `cache_read_input_tokens`. `text.sse` streams the same reply: message_start, the thinking
  block's start and deltas, the text block's, then the thinking block's empty signature_delta and
  every block's stop at the end, then message_delta and message_stop.
- `tool_use.json`: gpt-oss-120b asked "What is the weather in Paris right now?" with the tool
  `get_weather` (its description "Get the current temperature in a city, in degrees Celsius." and
  an object schema of a required string `city`), `tool_choice` `{"type":"any"}`, and `max_tokens`
  1024; a thinking block, its signature empty, then a tool_use block of input `{"city":"Paris"}`,
  stop_reason `tool_use`, sent with `stream` false as the format sends it. `tool_use.sse` streams
  the same reply, its tool_use block's start of no input member and its input_json deltas the
  model's text, `{\n  "city": "Paris"\n}`.
- `length.json`: Gemma 4 E4B asked "Count from 1 to 20, separated by spaces." with `max_tokens` 8;
  a text block, stop_reason `max_tokens`. `length.sse` streams the same text, its usage split
  otherwise between input_tokens and cache_read_input_tokens, their sum the same.
- `conversation.json`: Gemma 4 E4B with `system` as one text block, "Answer in one word.", and the
  messages a system message of "Use capital letters only.", the user's "What is the capital of
  France?", the assistant's "PARIS", and the user's "And of Italy?", with `max_tokens` 64; a
  text block, "ROME", stop_reason `end_turn`, showing the router takes a system message where it
  stands.
- `model_not_found.json`: the router's 400 body for a request naming
  `spike-ai-pipeline/no-such-model`, an OpenAI-style error, `{"error":{"code","message","type"}}`.

Anthropic's own error body, `{"type":"error","error":{"type","message"}}`, isn't recorded: the
tests write it inline, in the shape the Messages API documents.
