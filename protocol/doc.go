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
// type and constructor never carry the slug. When the spec adopts the feature, the client moves
// to the standard type and removes the extension: the wire discriminator and the formats'
// mappings change, and callers' Go code doesn't.
//
//   - Audio input: [InputAudio], wire type spike-ai-pipeline:input_audio, extending spec
//     2026-04-24. An audio input content part in the spec would replace it. A format whose API
//     has audio input of its own sends the clip in that API's form instead, as Chat Completions
//     does with input_audio.
//
// Its exported identifiers:
//
//   - [Request] is a request for a response: model, instructions, input, tools, tool choice,
//     reasoning, and the native tier.
//   - [ReasoningParam] configures the model's reasoning: its effort.
//   - [ReasoningEffort] is how much the model reasons: [ReasoningEffortNone],
//     [ReasoningEffortLow], [ReasoningEffortMedium], [ReasoningEffortHigh], or
//     [ReasoningEffortXHigh].
//   - [Request.Validate] refuses a request the model can't describe.
//   - [Response] is a model's response: output, status, and usage.
//   - [IncompleteDetails] says why a response is incomplete.
//   - [IncompleteReason] is that reason, [ReasonMaxOutputTokens] or [ReasonContentFilter].
//   - [Usage] counts a request's tokens.
//   - [OutputTokensDetails] breaks the output tokens down.
//   - [Status] is a response's or item's status, [StatusCompleted] or [StatusIncomplete].
//   - [Item] is one unit of a conversation, carried in Input and Output, a closed set.
//   - [Message] is a message item: content parts from a role.
//   - [Role] is a message's author: [RoleUser], [RoleSystem], [RoleDeveloper], or
//     [RoleAssistant].
//   - [ContentPart] is a part of a message's content, a closed set.
//   - [InputText] is text input, input_text.
//   - [InputImage] is image input, input_image, by URL or data URL.
//   - [ImageDetail] is an image's detail level: [ImageDetailAuto], [ImageDetailLow], or
//     [ImageDetailHigh].
//   - [DataURL] builds a base64 data URL from bytes and a media type.
//   - [InputAudio] is audio input, the client extension spike-ai-pipeline:input_audio.
//   - [AudioFormat] is a clip's encoding: [AudioFormatWAV] or [AudioFormatMP3].
//   - [OutputText] is text output, output_text.
//   - [Refusal] is the model's refusal, refusal.
//   - [FunctionCall] is a function_call item: the model's call of a function tool.
//   - [FunctionCallOutput] is a function_call_output item: a call's result, as a string or
//     parts.
//   - [Reasoning] is a reasoning item: the model's reasoning, as content, summary, and the
//     encrypted content its format owns.
//   - [ReasoningText] is reasoning the model produced, reasoning_text.
//   - [SummaryText] is a summary of the reasoning, summary_text.
//   - [FunctionTool] is a function the model may call, the spec's one tool kind.
//   - [ToolChoice] controls which tool the model calls, a closed set.
//   - [ToolChoiceMode] is a choice of mode, and an allowed set's: [ToolChoiceAuto],
//     [ToolChoiceNone], or [ToolChoiceRequired].
//   - [FunctionToolChoice] has the model call one named function.
//   - [AllowedToolChoice] narrows the tools the model may call to a set, with a mode.
package protocol
