package messages

import "encoding/json/jsontext"

// request is the Messages request body.
type request struct {
	Model string `json:"model"`
	// MaxTokens carries max_output_tokens. Messages requires it, so it is always sent.
	MaxTokens int `json:"max_tokens"`
	// System carries instructions, omitted when there are none.
	System   string    `json:"system,omitzero"`
	Messages []message `json:"messages"`
	Tools    []tool    `json:"tools,omitzero"`
	// ToolChoice carries the tool choice, omitted when the request sets none.
	ToolChoice *toolChoice `json:"tool_choice,omitzero"`
	// Thinking carries reasoning.effort's switch: disabled for none, adaptive for any other.
	Thinking *thinking `json:"thinking,omitzero"`
	// OutputConfig carries reasoning.effort's level and text.format's schema, omitted when the
	// request sets neither.
	OutputConfig *outputConfig `json:"output_config,omitzero"`
	// Stream is always sent, false for an unstreamed request, so a Native stream member clashes
	// with it.
	Stream bool `json:"stream"`
	// Native is the request's native tier, its members merged into the top level. A member
	// named like one the fields above set is a duplicate name, which encoding refuses.
	Native jsontext.Value `json:",embed"`
}

// tool is a Messages custom tool: its name, and its description, input schema, and strict flag.
// Messages requires input_schema, so it is always sent.
type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitzero"`
	InputSchema jsontext.Value `json:"input_schema"`
	Strict      bool           `json:"strict,omitzero"`
}

// toolChoice is a Messages tool_choice: of type auto, any, or none, or of type tool with the
// name of the tool the model must call.
type toolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitzero"`
}

// thinking is a Messages thinking configuration: of type disabled or adaptive.
type thinking struct {
	Type string `json:"type"`
}

// outputConfig is a Messages output_config: an effort level and an output format, each omitted
// when unset.
type outputConfig struct {
	Effort string        `json:"effort,omitzero"`
	Format *outputFormat `json:"format,omitzero"`
}

// outputFormat is an output_config's format: of type json_schema, with the schema the reply's
// text follows.
type outputFormat struct {
	Type   string         `json:"type"`
	Schema jsontext.Value `json:"schema"`
}

// message is a Messages message: its role, user, assistant, or system, and its content, always
// an array of blocks.
type message struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

// textBlock is a Messages text content block.
type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// thinkingBlock is a Messages thinking content block sent back: the thinking text and its
// signature, which Messages requires, so both are always sent.
type thinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

// redactedThinkingBlock is a Messages redacted_thinking content block sent back: its opaque
// data.
type redactedThinkingBlock struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// toolUseBlock is a Messages tool_use content block sent back: the call's id, the tool's name,
// and its input, a JSON value.
type toolUseBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input jsontext.Value `json:"input"`
}

// toolResultBlock is a Messages tool_result content block: the id of the tool_use it answers,
// and its content, a string or an array of text blocks.
type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   any    `json:"content"`
}

// imageBlock is a Messages image content block.
type imageBlock struct {
	Type   string      `json:"type"`
	Source imageSource `json:"source"`
}

// imageSource is an image block's source: of type base64, with the image's media type and its
// bytes in standard base64, or of type url, with the URL the endpoint fetches.
type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitzero"`
	Data      string `json:"data,omitzero"`
	URL       string `json:"url,omitzero"`
}

// response is the part of a Messages response body the format reads. Members it doesn't read,
// such as model and stop_sequence, are ignored.
type response struct {
	// ID is the reply's id, such as msg_abc, from which the format derives its items' ids.
	ID         string  `json:"id"`
	Content    []block `json:"content"`
	StopReason string  `json:"stop_reason"`
	Usage      usage   `json:"usage"`
}

// block is the part of a reply's content block the format reads: its type; a text block's text;
// a thinking block's thinking and signature; a redacted_thinking block's data; and a tool_use
// block's id, name, and input.
type block struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	Thinking  string         `json:"thinking"`
	Signature string         `json:"signature"`
	Data      string         `json:"data"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     jsontext.Value `json:"input"`
}

// usage is a reply's token counts. InputTokens counts the input tokens neither read from nor
// written to the prompt cache, which the two cache counts count.
type usage struct {
	InputTokens              int `json:"input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

// envelopePrefix opens every envelope this format writes: the implementor slug, the format, and
// the envelope's version. A Reasoning item's encrypted_content without it was written by another
// format, or by another version of this one, and isn't this format's to read.
const envelopePrefix = "spike-ai-pipeline:messages/v1:"

// envelope is what this format writes into a Reasoning item's encrypted_content when it decodes
// a reply, and reads back when it encodes the item: the block it must resend for the reasoning.
// Its layout is envelopePrefix followed by the envelope as JSON in standard, padded base64, of
// one of two shapes: {"thinking","signature"} for a thinking block, both members always present,
// and {"data"} for a redacted_thinking block:
//
//	spike-ai-pipeline:messages/v1:eyJ0aGlua2luZyI6Ii4uLiIsInNpZ25hdHVyZSI6IiJ9
//
// The members are pointers so the shape shows in which are present: a thinking block's
// signature is present even when empty, as llama.cpp's always is. A change to the layout or the
// members' meaning is a new version, and a new prefix.
type envelope struct {
	// Thinking is a thinking block's thinking text.
	Thinking *string `json:"thinking,omitzero"`
	// Signature is a thinking block's signature, which Messages requires the block to carry
	// back.
	Signature *string `json:"signature,omitzero"`
	// Data is a redacted_thinking block's opaque data.
	Data *string `json:"data,omitzero"`
}

// streamEvent is the part of a streamed reply's event the format reads: its type, and the
// members of the event types it reads. Message is message_start's message, an object, or the
// string message of an error frame such as llama.cpp's.
type streamEvent struct {
	Type         string         `json:"type"`
	Message      jsontext.Value `json:"message"`
	Index        int            `json:"index"`
	ContentBlock block          `json:"content_block"`
	Delta        streamDelta    `json:"delta"`
	Usage        usage          `json:"usage"`
}

// streamMessage is the part of message_start's message the format reads: the reply's id and its
// usage, whose input counts the terminal Response takes.
type streamMessage struct {
	ID    string `json:"id"`
	Usage usage  `json:"usage"`
}

// streamDelta is a content_block_delta's delta, of a type and the member that type carries, or
// message_delta's, of the stop_reason.
type streamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	Signature   string `json:"signature"`
	PartialJSON string `json:"partial_json"`
	StopReason  string `json:"stop_reason"`
}
