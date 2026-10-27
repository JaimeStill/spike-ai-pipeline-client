# spike-ai-pipeline-client

The model-client layer of [spike-ai-pipeline](https://github.com/JaimeStill/spike-ai-pipeline):
Go primitives over the inference engines' native APIs, built under go-elemental as owned,
optimized infrastructure, on the standard library alone. It is four layers, importing one way:
`protocol`, the request and response model, Open Responses (spec 2026-04-24) in its stateless
subset with client extensions; `format`, the contract a wire format meets; a package per wire
format below it, Chat Completions (`format/completions`) and Anthropic Messages
(`format/messages`), with Open Responses to come; and the module root, the transport, which speaks
the format chosen by constructor at the composition root.

It has four operations: `Generate`, chat with text, image, and audio parts, tools, structured
output, and reasoning; `GenerateStream`, the same request streamed; `Embed`; and `Transcribe`.

```go
c, err := client.New(client.Config{
	BaseURL: "http://localhost:8080/v1",
	HTTP:    http.DefaultClient,
	Format:  completions.New(),
})
if err != nil {
	return err
}
resp, err := c.Generate(ctx, &protocol.Request{
	Model: "gemma",
	Input: []protocol.Item{protocol.Message{
		Role:    protocol.RoleUser,
		Content: []protocol.ContentPart{protocol.InputText{Text: "Capital of France?"}},
	}},
})
```

The format is the Config's `Format`, and the same request goes out in either. `completions.New()`
speaks Chat Completions, `/chat/completions`, the OpenAI-compatible shape. `messages.New()` speaks
Anthropic Messages, `/messages`, at API version 2026-10-21, which every request names in its
`anthropic-version` header; it targets Anthropic's API and the endpoints compatible with it, such
as llama.cpp's router. Messages requires `max_tokens`, so it refuses a request with no
`MaxOutputTokens`, and it refuses audio input, which it has no form for. The hints it has no form
for are dropped, never refused: an image's detail, text's verbosity, and a JSON-schema format's
name, description, and strict flag. A reasoning item goes back to the format that wrote it through
its `encrypted_content`, and one another format wrote is dropped, so a conversation can move from
one format to the other. llama.cpp's router reads less of a Messages body than Anthropic's API
does: it drops reasoning effort's level and structured output's schema, and a named or none tool
choice reaches the model as any or none at all; the `format/messages` package documentation
lists what it drops.

```go
c, err := client.New(client.Config{
	BaseURL: "http://localhost:8080/v1",
	HTTP:    http.DefaultClient,
	Format:  messages.New(),
})
```

`GenerateStream` yields the response as Open Responses' streaming events, read from the
endpoint's server-sent events as they arrive, in a lazy `iter.Seq2` that sends the request when it
is ranged. A `protocol.Accumulator` folds the events, in the caller's loop, into the Response
`Generate` would return; after a stream that breaks or is cancelled, it holds the partial output.

```go
var acc protocol.Accumulator
for event, err := range c.GenerateStream(ctx, req) {
	if err != nil {
		return err // acc.Response() holds the output so far, in progress
	}
	if d, ok := event.(protocol.OutputTextDelta); ok {
		fmt.Print(d.Delta)
	}
	if err := acc.Add(event); err != nil {
		return err
	}
}
resp := acc.Response()
```

The library sets no timeout, retry, or limit unless the caller does. Timeouts are the context's
and the caller's transport's. Retry is opt-in: `Config.Retry`, a `RetryPolicy` of the most
attempts and the backoff's base and most delays, sends a request again on a 408, 429, or 503
reply, or a connection failure before any reply, honoring Retry-After, and never retries another
status, a started stream, or `Transcribe`.

The package documentation is the API reference. The module is released by tag, `vX.Y.Z`, with a
CHANGELOG, and spike-ai-pipeline-processing and the root build on it by tag.

The spike's question, evidence, and answer are in the root repository's README.

Apache-2.0.
