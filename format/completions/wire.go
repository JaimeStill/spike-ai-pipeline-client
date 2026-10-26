package completions

import "encoding/json/jsontext"

// request is the Chat Completions request body.
type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	// MaxCompletionTokens carries max_output_tokens. llama.cpp b11669 reads it and max_tokens
	// alike, as aliases of n_predict; max_completion_tokens is the name OpenAI keeps, having
	// deprecated max_tokens.
	MaxCompletionTokens int `json:"max_completion_tokens,omitzero"`
	// Native is the request's native tier, its members merged into the top level. A member
	// named like one the fields above set is a duplicate name, which encoding refuses.
	Native jsontext.Value `json:",embed"`
}

// message is a Chat Completions message. Content is a string when the message is a single text
// part, and an array of parts otherwise.
type message struct {
	Role    string         `json:"role"`
	Content jsontext.Value `json:"content"`
}

// textPart is a Chat Completions text content part.
type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
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
			Content string `json:"content"`
			Refusal string `json:"refusal"`
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
