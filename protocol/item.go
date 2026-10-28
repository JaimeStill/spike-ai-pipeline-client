package protocol

import "encoding/base64"

// Item is one unit of a conversation, as Open Responses defines it: a message, a function call
// or its output, or reasoning. Items are carried in a request's Input and a response's
// Output, and the same value moves from one response's Output into the next request's Input,
// so a caller continues a conversation by appending what came back. The set is closed: the item
// types are this package's, and an item is one of them by value, such as Message, never a
// pointer to one.
type Item interface {
	item()
}

// Message is a message item: content from a role. User messages hold InputText, InputImage,
// and InputAudio parts; system and developer messages hold InputText parts, as the spec allows
// them no other; assistant messages hold OutputText and Refusal parts.
type Message struct {
	// ID is the item's id in a response's output, which the spec requires there and the
	// streaming events carry as their item id. A format whose API supplies none derives it, as
	// completions does from the reply's id and the item's output index; it is empty when the
	// format has nothing to derive it from.
	ID string
	// Status is the item's status in a response's output, when the format supplies one. It is
	// ignored on input.
	Status Status
	// Role is the message's author.
	Role Role
	// Content is the message's content parts, at least one.
	Content []ContentPart
}

func (Message) item() {}

// Role is the author of a message.
type Role string

// The roles a message takes.
const (
	// RoleUser is the end user's input.
	RoleUser Role = "user"
	// RoleSystem is system-level instructions that set global behavior.
	RoleSystem Role = "system"
	// RoleDeveloper is instructions from the application's developer.
	RoleDeveloper Role = "developer"
	// RoleAssistant is the model's output, or a prior turn of it sent back as input.
	RoleAssistant Role = "assistant"
)

// ContentPart is one part of an item's content: of a message's, or ReasoningText, of a reasoning
// item's, which the spec's content part events carry beside a message's parts. The set is closed:
// the part types are this package's, and a part is one of them by value, such as InputText.
type ContentPart interface {
	contentPart()
}

// InputText is text input to the model: input_text.
type InputText struct {
	// Text is the input.
	Text string
}

func (InputText) contentPart() {}

// InputImage is an image input to the model: input_image.
type InputImage struct {
	// ImageURL locates the image: a fully qualified URL the endpoint fetches, or the image
	// itself as a base64 data URL, which DataURL builds from its bytes. It is required.
	ImageURL string
	// Detail is the resolution the model sees the image at. Empty leaves it to the endpoint,
	// whose default is ImageDetailAuto.
	Detail ImageDetail
}

func (InputImage) contentPart() {}

// ImageDetail is the resolution at which the model sees an InputImage.
type ImageDetail string

// The detail levels an InputImage takes.
const (
	// ImageDetailAuto lets the endpoint choose the detail level.
	ImageDetailAuto ImageDetail = "auto"
	// ImageDetailLow restricts the model to a lower-resolution version of the image.
	ImageDetailLow ImageDetail = "low"
	// ImageDetailHigh lets the model see a higher-resolution version of the image, usually at a
	// higher input token cost.
	ImageDetailHigh ImageDetail = "high"
)

// DataURL returns data as a base64 data URL of mediaType, such as "image/png", for an
// InputImage's ImageURL: "data:image/png;base64,...".
func DataURL(mediaType string, data []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// InputAudio is an audio clip input to the model. It is a client extension, the spec having no
// audio input part: on the Open Responses wire its type is spike-ai-pipeline:input_audio, and
// the package comment lists it with the standard feature that would replace it.
type InputAudio struct {
	// Data is the clip's bytes, in Format. It is required.
	Data []byte
	// Format is the clip's encoding. It is required.
	Format AudioFormat
}

func (InputAudio) contentPart() {}

// AudioFormat is the encoding of an InputAudio clip.
type AudioFormat string

// The encodings an InputAudio clip takes.
const (
	// AudioFormatWAV is a RIFF WAVE file.
	AudioFormatWAV AudioFormat = "wav"
	// AudioFormatMP3 is an MPEG-1 Audio Layer III file.
	AudioFormatMP3 AudioFormat = "mp3"
)

// OutputText is text the model produced: output_text.
type OutputText struct {
	// Text is the output.
	Text string
}

func (OutputText) contentPart() {}

// Refusal is the model's refusal to answer: refusal.
type Refusal struct {
	// Refusal is the model's explanation of the refusal.
	Refusal string
}

func (Refusal) contentPart() {}

// FunctionCall is a function_call item: the model's call of a function tool the request
// declared. It comes back in a response's Output, and goes back in the next request's Input,
// ahead of the FunctionCallOutput that answers it.
type FunctionCall struct {
	// ID is the item's id in a response's output, which the spec requires there and the
	// streaming events carry as their item id. A format whose API supplies none derives it, as
	// completions does from the reply's id and the item's output index; it is empty when the
	// format has nothing to derive it from.
	ID string
	// Status is the item's status in a response's output, when the format supplies one. It is
	// ignored on input.
	Status Status
	// CallID is the model's id for the call, which the FunctionCallOutput answering it
	// repeats. It is required.
	CallID string
	// Name is the name of the function called. It is required.
	Name string
	// Arguments is the call's arguments as JSON text, such as `{"city":"Paris"}`, as the
	// model produced them. Empty means no arguments; otherwise it is valid JSON.
	Arguments string
}

func (FunctionCall) item() {}

// FunctionCallOutput is a function_call_output item: the result of a FunctionCall, which the
// caller ran, sent back as input. The spec's output is a string or an array of content parts,
// and the item holds either: Output for the string, OutputParts for the array. OutputParts
// takes the spec's part types for it in the stateless subset this client covers, InputText
// alone; it is a []ContentPart rather than a []InputText so the image and file parts the spec
// also allows there can join it without changing the field.
type FunctionCallOutput struct {
	// ID is the item's id, when the format supplies one.
	ID string
	// Status is the item's status, when the format supplies one. It is ignored on input.
	Status Status
	// CallID is the CallID of the FunctionCall this answers. It is required.
	CallID string
	// Output is the function's result as a string, such as JSON text. It is empty when
	// OutputParts holds the result instead; an empty Output with no OutputParts is an empty
	// result.
	Output string
	// OutputParts is the function's result as content parts, InputText, in place of Output.
	// When it is set, Output is empty.
	OutputParts []ContentPart
}

func (FunctionCallOutput) item() {}

// Reasoning is a reasoning item: the model's reasoning ahead of its answer or its calls. It
// comes back in a response's Output ahead of the items it led to, and goes back in the next
// request's Input as it came, so a format that must resend the reasoning can.
//
// Content is what a caller reads; EncryptedContent is what a format resends. The spec's input
// reasoning item has no content, so a format never sends Content on input: it ignores it there
// and reads EncryptedContent instead. EncryptedContent is opaque and owned by the format that
// wrote it, which writes there what it needs to resend the reasoning, in a form of its own; a
// caller carries it back unchanged. A format reading another format's EncryptedContent drops
// the item rather than send it.
//
// One exception sends reasoning text on input: the responses format's ResendReasoningText
// option, for an endpoint that reads the reasoning from an input item's content, as llama.cpp's
// router does. Even then Content is ignored: the text sent is the one the format wrote into
// EncryptedContent.
//
// Reasoning has a Status, as every other output item does. The spec's reference schema for the
// reasoning item omits status, but its normative rule for items requires id, type, and status on
// every item, and its own reasoning example carries one; the client follows the rule.
type Reasoning struct {
	// ID is the item's id in a response's output, which the spec requires there and the
	// streaming events carry as their item id. A format whose API supplies none derives it, as
	// completions does from the reply's id and the item's output index; it is empty when the
	// format has nothing to derive it from.
	ID string
	// Status is the item's status in a response's output, when the format supplies one. It is
	// ignored on input.
	Status Status
	// Content is the reasoning the model produced, as reasoning_text parts, when the format
	// supplies it. It is output only: on input it is ignored and never sent, the reasoning text
	// that the responses format's ResendReasoningText sends coming from EncryptedContent.
	Content []ReasoningText
	// Summary is a summary of the reasoning, as summary_text parts, when the format supplies
	// one. Chat Completions doesn't.
	Summary []SummaryText
	// EncryptedContent is the reasoning in the form of the format that wrote it, opaque to the
	// caller, which carries it back unchanged.
	EncryptedContent string
}

func (Reasoning) item() {}

// ReasoningText is reasoning the model produced: reasoning_text, a part of a Reasoning item's
// Content. It is a ContentPart so the content part events of a streamed reasoning item can carry
// it, as the spec's do, but a message never holds it: Validate refuses it there.
type ReasoningText struct {
	// Text is the reasoning.
	Text string
}

func (ReasoningText) contentPart() {}

// SummaryText is a summary of the model's reasoning: summary_text, a part of a Reasoning
// item's Summary. It is held there alone, so it isn't a ContentPart.
type SummaryText struct {
	// Text is the summary.
	Text string
}

// Status is the status of a response or of an item in its output.
type Status string

// The statuses a stateless response and its items take, under the spec's names.
const (
	// StatusInProgress is a response or item the model is still producing: one a stream has
	// begun and not finished.
	StatusInProgress Status = "in_progress"
	// StatusCompleted is a response or item the model finished.
	StatusCompleted Status = "completed"
	// StatusIncomplete is a response or item the model stopped before finishing; a response's
	// IncompleteDetails says why.
	StatusIncomplete Status = "incomplete"
	// StatusFailed is a response that failed before the model finished it, as a stream's
	// terminal response.failed carries it.
	StatusFailed Status = "failed"
)
