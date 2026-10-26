# spike-ai-pipeline-client

The model-client layer of [spike-ai-pipeline](https://github.com/JaimeStill/spike-ai-pipeline):
Go primitives over the inference engines' native APIs, built under go-elemental as owned,
optimized infrastructure, on the standard library alone. It is four layers, importing one way:
`protocol`, the request and response model, Open Responses (spec 2026-04-24) in its stateless
subset with client extensions; `format`, the contract a wire format meets; a package per wire
format below it, Chat Completions (`format/completions`) first, then Anthropic Messages and Open
Responses; and the module root, the transport, which speaks the format chosen by constructor at
the composition root.

It has three operations: `Generate`, chat with text, image, and audio parts, tools, structured
output, and reasoning; `Embed`; and `Transcribe`.

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

The package documentation is the API reference. The module is released by tag, `vX.Y.Z`, with a
CHANGELOG, and spike-ai-pipeline-processing and the root build on it by tag.

The spike's question, evidence, and answer are in the root repository's README.

Apache-2.0.
