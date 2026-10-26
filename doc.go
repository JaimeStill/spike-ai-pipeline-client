// Package client is the model client of spike-ai-pipeline: Go primitives over the inference
// engines' native APIs, with the wire format a pluggable layer beneath a stable request and
// response model. README.md states its role in the spike.
//
// The client presents model inference in two tiers. The standard tier is Open Responses' item
// model (https://www.openresponses.org, spec 2026-04-24), in its stateless subset, using the
// spec's names: a Request of input items, a Response of output items. The native tier is the
// request's Native field, raw JSON merged into the wire body as it is, for what the standard
// tier doesn't cover. Each wire format is an adapter beneath the Format interface, in a package
// of its own named for the API it targets, such as chatcompletions, chosen by its constructor
// at the caller's composition root.
//
// The library ships no policy: no default model, token budget, timeout, retry, or size limit.
// The caller's http.Client and context carry them.
//
// Its exported identifiers:
//
//   - [Config] describes an endpoint: base URL, query, token source, http.Client, and format.
//   - [TokenSource] returns the bearer token for one request.
//   - [New] validates a Config and returns a Client.
//   - [Client] makes requests of one endpoint in one format.
//   - [Client.Respond] sends a Request and returns its Response.
//   - [Format] is a wire format, the adapter between the model and one API's shape.
//   - [APIError] is a non-2xx reply, matched with errors.As.
//   - [Request] is a request for a response: model, instructions, input, and the native tier.
//   - [Response] is a model's response: output, status, and usage.
//   - [IncompleteDetails] says why a response is incomplete.
//   - [IncompleteReason] is that reason, [ReasonMaxOutputTokens] or [ReasonContentFilter].
//   - [Usage] counts a request's tokens.
//   - [OutputTokensDetails] breaks the output tokens down.
//   - [Status] is a response's or item's status, [StatusCompleted] or [StatusIncomplete].
//   - [Item] is an input or output item, a closed set.
//   - [Message] is a message item: content parts from a role.
//   - [Role] is a message's author: [RoleUser], [RoleSystem], [RoleDeveloper], or
//     [RoleAssistant].
//   - [ContentPart] is a part of a message's content, a closed set.
//   - [InputText] is text input, input_text.
//   - [OutputText] is text output, output_text.
//   - [Refusal] is the model's refusal, refusal.
package client
