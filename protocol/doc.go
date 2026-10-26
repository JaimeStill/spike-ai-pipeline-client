// Package protocol is the client's request and response model: Open Responses' item model
// (https://www.openresponses.org, spec 2026-04-24), in its stateless subset, using the spec's
// names. A Request of input items goes in and a Response of output items comes out; a wire
// format converts them to and from its API's shape, and the client sends them. The package
// imports neither, so a caller builds requests and items without the transport.
//
// The standard tier is the spec's model. The native tier is a Request's Native field, raw JSON
// merged into the wire body's top level as it is, for what the standard tier doesn't cover.
//
// # Client extensions
//
// A client extension is a feature the spec doesn't yet define, added under the spec's extension
// rule: its wire type is prefixed with the implementor slug, spike-ai-pipeline. Each is listed
// here with the spec version it extends and the standard feature that would replace it. Its Go
// type and constructor never carry the slug, so moving to the standard feature changes only the
// wire discriminator and the formats' mappings, not callers' code. There are none yet.
//
// Its exported identifiers:
//
//   - [Request] is a request for a response: model, instructions, input, and the native tier.
//   - [Request.Validate] refuses a request the model can't describe.
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
package protocol
