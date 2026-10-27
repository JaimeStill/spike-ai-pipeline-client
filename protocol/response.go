package protocol

// Response is a model's response: Open Responses' response object, in its stateless subset.
type Response struct {
	// Output is what the model produced, in order.
	Output []Item
	// Status is StatusCompleted, or StatusIncomplete when the model stopped early. A streamed
	// response is StatusInProgress until its stream finishes, and StatusFailed when its stream
	// ends with response.failed.
	Status Status
	// IncompleteDetails says why the response is incomplete. It is nil when it is complete.
	IncompleteDetails *IncompleteDetails
	// Error is the error the response failed with, the spec's error member. It is set on a
	// response in StatusFailed, and nil otherwise.
	Error *ResponseError
	// Usage counts the tokens the request used.
	Usage Usage
}

// ResponseError is the error a failed response holds: the spec's Error object, which a stream's
// response.failed carries in its Response.
type ResponseError struct {
	// Code is the machine-readable error code. The spec requires one; it is empty when the
	// endpoint gave none.
	Code string
	// Message is the human-readable description of the error.
	Message string
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
