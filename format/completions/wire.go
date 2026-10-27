package completions

import "encoding/json/jsontext"

// request is the Chat Completions request body.
type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Tools    []tool    `json:"tools,omitzero"`
	// ToolChoice is "auto", "none", "required", or a namedToolChoice or allowedToolsChoice
	// object.
	ToolChoice jsontext.Value `json:"tool_choice,omitzero"`
	// ReasoningEffort carries reasoning.effort.
	ReasoningEffort string `json:"reasoning_effort,omitzero"`
	// ResponseFormat carries text.format.
	ResponseFormat *responseFormat `json:"response_format,omitzero"`
	// Verbosity carries text.verbosity.
	Verbosity string `json:"verbosity,omitzero"`
	// MaxCompletionTokens carries max_output_tokens. llama.cpp b11669 reads it and max_tokens
	// alike, as aliases of n_predict; max_completion_tokens is the name OpenAI keeps, having
	// deprecated max_tokens.
	MaxCompletionTokens int `json:"max_completion_tokens,omitzero"`
	// Stream is always sent, true for a streamed request and false otherwise, so a Native
	// stream member clashes with it in both.
	Stream bool `json:"stream"`
	// StreamOptions asks a streamed reply for its usage. It is sent with a streamed request
	// alone, as OpenAI refuses it otherwise.
	StreamOptions *streamOptions `json:"stream_options,omitzero"`
	// Native is the request's native tier, its members merged into the top level. A member
	// named like one the fields above set is a duplicate name, which encoding refuses.
	Native jsontext.Value `json:",embed"`
}

// streamOptions is a streamed request's stream_options. IncludeUsage asks for the usage, in a
// chunk of no choices after the one with the finish_reason.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// message is a Chat Completions message. Content is a string when the message is a single text
// part, and an array of parts otherwise; an assistant message of tool calls alone omits it.
// ReasoningContent is the reasoning that led to an assistant message, the field llama.cpp and
// other OpenAI-compatible servers name so. ToolCalls are an assistant message's calls, and
// ToolCallID names the call a tool message answers.
type message struct {
	Role             string         `json:"role"`
	Content          jsontext.Value `json:"content,omitzero"`
	ReasoningContent string         `json:"reasoning_content,omitzero"`
	ToolCalls        []toolCall     `json:"tool_calls,omitzero"`
	ToolCallID       string         `json:"tool_call_id,omitzero"`
}

// tool is a Chat Completions tool, always a function.
type tool struct {
	Type     string       `json:"type"`
	Function functionTool `json:"function"`
}

// functionTool is a tool's function: its name, and its description, parameters, and strict
// flag, each omitted when unset.
type functionTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitzero"`
	Parameters  jsontext.Value `json:"parameters,omitzero"`
	Strict      bool           `json:"strict,omitzero"`
}

// toolCall is a Chat Completions tool call, always a function's, in an assistant message of a
// request or a reply.
type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

// functionCall is a tool call's function: its name and its arguments as JSON text.
type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// namedToolChoice is a tool_choice that has the model call one function.
type namedToolChoice struct {
	Type     string       `json:"type"`
	Function functionName `json:"function"`
}

// functionName names a function in a tool choice.
type functionName struct {
	Name string `json:"name"`
}

// allowedToolsChoice is a tool_choice that narrows the tools to a set.
type allowedToolsChoice struct {
	Type         string       `json:"type"`
	AllowedTools allowedTools `json:"allowed_tools"`
}

// allowedTools is an allowed_tools choice's set: its mode, auto or required, and its tools,
// each a namedToolChoice.
type allowedTools struct {
	Mode  string            `json:"mode"`
	Tools []namedToolChoice `json:"tools"`
}

// responseFormat is a Chat Completions response_format: of type text, or of type json_schema
// with its JSONSchema.
type responseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *jsonSchema `json:"json_schema,omitzero"`
}

// jsonSchema is a json_schema response format's schema: its name and schema, and its description
// and strict flag, each omitted when unset.
type jsonSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitzero"`
	Schema      jsontext.Value `json:"schema"`
	Strict      bool           `json:"strict,omitzero"`
}

// textPart is a Chat Completions text content part.
type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// imagePart is a Chat Completions image content part.
type imagePart struct {
	Type     string   `json:"type"`
	ImageURL imageURL `json:"image_url"`
}

// imageURL is an image part's image: its URL, a remote URL or a data URL, and its detail level,
// omitted when unset.
type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitzero"`
}

// audioPart is a Chat Completions audio content part.
type audioPart struct {
	Type       string     `json:"type"`
	InputAudio inputAudio `json:"input_audio"`
}

// inputAudio is an audio part's clip: its bytes in standard base64, and its format.
type inputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

// refusalPart is a Chat Completions refusal content part.
type refusalPart struct {
	Type    string `json:"type"`
	Refusal string `json:"refusal"`
}

// response is the part of a Chat Completions response body the format reads. Members it
// doesn't read, such as llama.cpp's timings, are ignored.
type response struct {
	Choices []struct {
		Message struct {
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			Refusal          string     `json:"refusal"`
			ToolCalls        []toolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage usage `json:"usage"`
}

// usage is a reply's token counts, in a response body or a stream's chunk.
type usage struct {
	PromptTokens            int `json:"prompt_tokens"`
	CompletionTokens        int `json:"completion_tokens"`
	TotalTokens             int `json:"total_tokens"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// chunk is the part of a streamed reply's chunk, chat.completion.chunk, the format reads. A
// null finish_reason or delta member decodes as empty. Usage is set on the chunk that carries
// it, with stream_options.include_usage the last, of no choices. Members it doesn't read, such
// as llama.cpp's timings, are ignored.
type chunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usage `json:"usage"`
}

// envelopePrefix opens every envelope this format writes: the implementor slug, the format, and
// the envelope's version. A Reasoning item's encrypted_content without it was written by another
// format, or by another version of this one, and isn't this format's to read.
const envelopePrefix = "spike-ai-pipeline:completions/v1:"

// envelope is what this format writes into a Reasoning item's encrypted_content when it decodes
// a reply, and reads back when it encodes the item: what it must resend for the reasoning. Its
// layout is envelopePrefix followed by the envelope as JSON in standard, padded base64:
//
//	spike-ai-pipeline:completions/v1:eyJyZWFzb25pbmdfY29udGVudCI6Ii4uLiJ9
//
// A change to the layout or the members' meaning is a new version, and a new prefix.
type envelope struct {
	// ReasoningContent is the reply's reasoning_content, which goes back as the assistant
	// message's.
	ReasoningContent string `json:"reasoning_content"`
}
