package client

// Response is a model's response: Open Responses' response object, in its stateless subset.
type Response struct {
	// Output is what the model produced, in order.
	Output []Item
	// Status is StatusCompleted, or StatusIncomplete when the model stopped early.
	Status Status
	// IncompleteDetails says why the response is incomplete. It is nil when it is complete.
	IncompleteDetails *IncompleteDetails
	// Usage counts the tokens the request used.
	Usage Usage
}

// IncompleteDetails says why a response is incomplete.
type IncompleteDetails struct {
	// Reason is why the model stopped.
	Reason IncompleteReason
}

// IncompleteReason is why a response is incomplete.
type IncompleteReason string

// The reasons a response is incomplete.
const (
	// ReasonMaxOutputTokens is a response cut off at the output token bound.
	ReasonMaxOutputTokens IncompleteReason = "max_output_tokens"
	// ReasonContentFilter is a response stopped by the endpoint's content filter.
	ReasonContentFilter IncompleteReason = "content_filter"
)

// Usage counts the tokens a request used, as the endpoint reports them.
type Usage struct {
	// InputTokens counts the tokens of the input, instructions included.
	InputTokens int
	// OutputTokens counts the tokens generated, reasoning included.
	OutputTokens int
	// TotalTokens counts input and output tokens together.
	TotalTokens int
	// OutputTokensDetails breaks OutputTokens down.
	OutputTokensDetails OutputTokensDetails
}

// OutputTokensDetails breaks a response's output tokens down.
type OutputTokensDetails struct {
	// ReasoningTokens counts the output tokens spent reasoning, when the endpoint reports
	// them.
	ReasoningTokens int
}
