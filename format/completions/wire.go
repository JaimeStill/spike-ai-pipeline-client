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
	// MaxCompletionTokens carries max_output_tokens. llama.cpp b11669 reads it and max_tokens
	// alike, as aliases of n_predict; max_completion_tokens is the name OpenAI keeps, having
	// deprecated max_tokens.
	MaxCompletionTokens int `json:"max_completion_tokens,omitzero"`
	// Native is the request's native tier, its members merged into the top level. A member
	// named like one the fields above set is a duplicate name, which encoding refuses.
	Native jsontext.Value `json:",embed"`
}

// message is a Chat Completions message. Content is a string when the message is a single text
// part, and an array of parts otherwise; an assistant message of tool calls alone omits it.
// ToolCalls are an assistant message's calls, and ToolCallID names the call a tool message
// answers.
type message struct {
	Role       string         `json:"role"`
	Content    jsontext.Value `json:"content,omitzero"`
	ToolCalls  []toolCall     `json:"tool_calls,omitzero"`
	ToolCallID string         `json:"tool_call_id,omitzero"`
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
// doesn't read, such as reasoning_content and llama.cpp's timings, are ignored.
type response struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			Refusal   string     `json:"refusal"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens            int `json:"prompt_tokens"`
		CompletionTokens        int `json:"completion_tokens"`
		TotalTokens             int `json:"total_tokens"`
		CompletionTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

// errorBody is an OpenAI-style error body. Error is an errorObject or, from some servers, a bare
// string.
type errorBody struct {
	Error jsontext.Value `json:"error"`
}

// errorObject is the object form of an error body's error. Code is a string or a number.
type errorObject struct {
	Message string         `json:"message"`
	Type    string         `json:"type"`
	Code    jsontext.Value `json:"code"`
}
