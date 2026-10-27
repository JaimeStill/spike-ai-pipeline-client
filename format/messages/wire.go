package messages

// request is the Messages request body.
type request struct {
	Model string `json:"model"`
	// MaxTokens carries max_output_tokens. Messages requires it, so it is always sent.
	MaxTokens int `json:"max_tokens"`
	// System carries instructions, omitted when there are none.
	System   string    `json:"system,omitzero"`
	Messages []message `json:"messages"`
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

// block is the part of a reply's content block the format reads: its type, and a text block's
// text.
type block struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// usage is a reply's token counts. InputTokens counts the input tokens neither read from nor
// written to the prompt cache, which the two cache counts count.
type usage struct {
	InputTokens              int `json:"input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}
