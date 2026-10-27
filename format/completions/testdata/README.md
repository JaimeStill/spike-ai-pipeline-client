# completions testdata

Chat Completions replies recorded from the llama.cpp router on gb10-2 (b11669, set A), each
streamed (`.sse`, the body as it arrived) and unstreamed (`.json`) for the same request, sent with
`temperature` 0 and `seed` 7 so the two replies agree. The stream tests translate each `.sse` and
fold its events, and compare the result with what `DecodeResponse` gives for the `.json`.

- `text.sse`, `text.json`: gpt-oss-120b, reasoning effort low, asked "Name three French cities,
  comma-separated, nothing else."; reasoning_content, then content, finish_reason `stop`, and
  usage in a last chunk of no choices.
- `length.sse`, `length.json`: Gemma 4 E4B asked to count from 1 to 20 with
  `max_completion_tokens` 8; content, then finish_reason `length`.

Each streamed request sent `"stream":true,"stream_options":{"include_usage":true}`, as
`EncodeStreamRequest` does, and each unstreamed one `"stream":false`, to
`/v1/chat/completions?autoload=false`.
