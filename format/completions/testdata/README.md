# completions testdata

Chat Completions replies recorded from the llama.cpp router on gb10-2 (b11669, set A), each
streamed (`.sse`, the body as it arrived) and unstreamed (`.json`) for the same request, sent with
`temperature` 0 and `seed` 7 so the two replies agree. The stream tests translate each `.sse` and
fold its events, and compare the result with what `DecodeResponse` gives for the `.json` given the
stream's id and its tool calls' ids: the two were separate requests, so their ids differ, and the
item ids derive from the reply's.

- `text.sse`, `text.json`: gpt-oss-120b, reasoning effort low, asked "Name three French cities,
  comma-separated, nothing else."; reasoning_content, then content, finish_reason `stop`, and
  usage in a last chunk of no choices.
- `length.sse`, `length.json`: Gemma 4 E4B asked to count from 1 to 20 with
  `max_completion_tokens` 8; content, then finish_reason `length`.
- `tool_call.sse`, `tool_call.json`: gpt-oss-120b, reasoning effort low, asked "What is the
  weather in Paris right now?" with the `get_weather` tool and `tool_choice` `required`;
  reasoning_content, then one tool call whose first fragment carries its id, type, name, and the
  arguments' first text, finish_reason `tool_calls`.
- `parallel_tool_calls.sse`, `parallel_tool_calls.json`: Gemma 4 26B A4B asked "What is the
  weather in Paris and in Tokyo right now? Call the tool once for each city, in parallel." with the
  same tool, `tool_choice` `required`, and `parallel_tool_calls` true; reasoning_content, then two
  tool calls, index 0 then index 1, finish_reason `tool_calls`. gpt-oss-120b made one call for the
  same request.

Each streamed request sent `"stream":true,"stream_options":{"include_usage":true}`, as
`EncodeStreamRequest` does, and each unstreamed one `"stream":false`, to
`/v1/chat/completions?autoload=false`.
