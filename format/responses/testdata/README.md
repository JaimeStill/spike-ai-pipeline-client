# responses testdata

Open Responses replies recorded from the llama.cpp router on gb10-2 (b11669, set A), each
unstreamed, for a request of the body `EncodeRequest` sends, `"stream":false`, `"store":false`,
and `"include":["reasoning.encrypted_content"]` among its members, posted to
`/v1/responses?autoload=false`. The successful ones were sent with `temperature` 0 and `seed` 7.
The decode tests compare what `DecodeResponse` gives for each reply with the response it should
be; the error tests read the error bodies.

- `text.json`: gpt-oss-120b with `instructions` "Answer with the names alone.", asked "Name three
  French cities, comma-separated, nothing else." with `max_output_tokens` 256; a reasoning item,
  its summary and `encrypted_content` empty, then a message of one output_text, every item's id
  the router's (`rs_`, `msg_`) and status `completed`.
- `tool_call.json`: gpt-oss-120b asked "What is the weather in Paris right now?" with one tool,
  `get_weather` (`strict` false), `tool_choice` "required", `reasoning` `{"effort":"medium"}`, and
  `max_output_tokens` 1024; a reasoning item, its summary and `encrypted_content` empty, then a
  function_call of `get_weather` with `{"city":"Paris"}`, every item's id the router's (`rs_`,
  `fc_`, and `call_` for its call_id) and status `completed`.
- `length.json`: Gemma 4 E4B asked "Count from 1 to 20, separated by spaces." with
  `max_output_tokens` 16; a message cut off at the bound, `output_tokens` 16, which the router
  reports with status `completed` and no `incomplete_details`.
- `conversation.json`: Gemma 4 E4B with `instructions` "Answer in one word." and the input a
  system message of "Use capital letters only.", the user's "What is the capital of France?", the
  assistant's output_text "PARIS", and the user's "And of Italy?", with `max_output_tokens` 64; a
  message, "ROME".
- `model_not_found.json`: the router's 400 body for a request naming
  `spike-ai-pipeline/no-such-model`, an OpenAI-style error of a numeric code.
- `input_audio.json`: the router's 400 body for a request to Gemma 4 E4B whose user message holds
  the audio extension, `{"type":"spike-ai-pipeline:input_audio","data","format":"wav"}`: the
  router takes no content part but input_text, input_image, and input_file.

- `text.sse` and `tool_call.sse`: the router's streams for the requests of `text.json` and
  `tool_call.json`, sent with `"stream":true`, as `EncodeStreamRequest` sends it, and recorded as
  they arrived. Each opens with response.created and response.in_progress, adds each item, streams
  the reasoning as `response.reasoning_text.delta`, adds the message's one part, streams its text
  and the call's arguments, and closes every item at the end, then response.completed, whose
  response holds the whole output and the usage. No event carries a `sequence_number`,
  `output_index`, or `content_index`; there are no reasoning part events, no reasoning done, and no
  function_call_arguments.done; and the reasoning item's output_item.done, like the terminal
  response's reasoning item, has no status. The stream tests compare what the events fold to with
  what `DecodeResponse` gives for the terminal event's response.

The spec's shapes the router doesn't send, such as a refusal, a summary, a non-empty
`encrypted_content`, and the incomplete and failed statuses, aren't recorded: the tests write them inline, in the shape the
spec's OpenAPI document gives.
