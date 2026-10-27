// Package protocol is the client's request and response model: Open Responses' item model
// (https://www.openresponses.org, spec 2026-04-24), in its stateless subset, using the spec's
// names. A Request of input items goes in and a Response of output items comes out; a wire
// format encodes the request in its API's shape and decodes the reply, and the client sends
// them. The package imports neither, so a caller builds requests and items without the
// transport.
//
// The standard tier is the spec's model. The native tier is a Request's Native field, raw JSON
// merged into the wire body's top level as it is, for what the standard tier doesn't cover.
//
// # Streaming
//
// A streamed response arrives as the spec's streaming events, the [Event] set, and an
// [Accumulator] folds them into the Response an unstreamed request returns. The subset is the
// state events response.created and the terminal response.completed, response.incomplete, and
// response.failed; the item events response.output_item.added and .done; the part events
// response.content_part.added and .done; the text events response.output_text.delta and .done,
// response.refusal.delta and .done, and response.reasoning.delta and .done, the last of a
// Reasoning item's reasoning_text part; and the argument events
// response.function_call_arguments.delta and .done. It leaves out response.queued and
// response.in_progress, reasoning summary events, output_text annotations, logprobs, and the
// deltas' obfuscation padding.
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
//     reasoning, text, and the native tier.
//   - [TextParam] configures the model's text output: its format and verbosity.
//   - [TextFormat] is the form of the model's output_text, a closed set.
//   - [PlainTextFormat] has the model answer in plain text, the text format.
//   - [JSONSchemaFormat] has the model answer with JSON a schema describes, the json_schema
//     format.
//   - [Verbosity] is how much detail the model's text gives: [VerbosityLow],
//     [VerbosityMedium], or [VerbosityHigh].
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
//   - [Status] is a response's or item's status, [StatusInProgress], [StatusCompleted], or
//     [StatusIncomplete].
//   - [Item] is one unit of a conversation, carried in Input and Output, a closed set.
//   - [Message] is a message item: content parts from a role.
//   - [Role] is a message's author: [RoleUser], [RoleSystem], [RoleDeveloper], or
//     [RoleAssistant].
//   - [ContentPart] is a part of a message's content, or of a reasoning item's, a closed set.
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
//   - [Event] is one of a streamed response's events, a closed set.
//   - [ResponseCreated] is response.created, the event that opens a stream.
//   - [ResponseCompleted] is response.completed, the terminal event of a finished response.
//   - [ResponseIncomplete] is response.incomplete, the terminal event of a response the model
//     stopped early.
//   - [ResponseFailed] is response.failed, the terminal event of a failed response.
//   - [OutputItemAdded] is response.output_item.added, an output item begun.
//   - [OutputItemDone] is response.output_item.done, an output item finished, whole.
//   - [ContentPartAdded] is response.content_part.added, a content part begun.
//   - [ContentPartDone] is response.content_part.done, a content part finished, whole.
//   - [OutputTextDelta] is response.output_text.delta, text appended to an output_text part.
//   - [OutputTextDone] is response.output_text.done, an output_text part's whole text.
//   - [RefusalDelta] is response.refusal.delta, text appended to a refusal part.
//   - [RefusalDone] is response.refusal.done, a refusal part's whole text.
//   - [ReasoningDelta] is response.reasoning.delta, text appended to a reasoning_text part.
//   - [ReasoningDone] is response.reasoning.done, a reasoning_text part's whole text.
//   - [FunctionCallArgumentsDelta] is response.function_call_arguments.delta, argument text
//     appended to a function_call item.
//   - [FunctionCallArgumentsDone] is response.function_call_arguments.done, a function_call
//     item's whole arguments.
//   - [Accumulator] folds a stream's events into the Response they describe.
//   - [Accumulator.Add] folds the next event, refusing one that doesn't follow the events
//     before it.
//   - [Accumulator.Response] returns the Response folded so far.
package protocol
