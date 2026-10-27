package protocol

// Event is one of Open Responses' streaming events: a semantic event of a streamed response,
// under the spec's name. A stream opens with ResponseCreated and closes with one terminal event,
// ResponseCompleted, ResponseIncomplete, or ResponseFailed, each carrying the Response. Between
// them each output item is added, its content parts are added, streamed as deltas, and done, and
// the item is done, in that order: a Message's parts are OutputText and Refusal, and a
// Reasoning item's ReasoningText; a FunctionCall, which has no content parts, streams its
// arguments as deltas and their done event between being added and being done. An Accumulator
// folds the events back into the Response. A failure the server reports is the error event,
// ErrorEvent, which response.failed follows, as the spec has every streaming error followed. The
// set is closed: the event types are this package's, and an event is one of them by value, such
// as OutputTextDelta, never a pointer to one.
//
// Every event carries its SequenceNumber, which increases through the stream. An event about an
// output item carries the item's OutputIndex, its place in the Response's Output; an event about
// a content part, or about a function call's arguments, carries the item's ItemID, the ID of the
// item as added; and an event about a content part carries the part's ContentIndex, its place in
// the item's content. The spec requires the item id there; a format
// whose API supplies none derives one, as completions does, and leaves it empty only when it has
// nothing to derive it from.
type Event interface {
	event()
}

// ResponseCreated is response.created: the response was created, the first event of a stream.
type ResponseCreated struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// Response is the response as created, its status StatusInProgress and its output empty.
	Response *Response
}

func (ResponseCreated) event() {}

// ResponseCompleted is response.completed: the response finished, a terminal event.
type ResponseCompleted struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// Response is the finished response, its status StatusCompleted, with its output and usage.
	Response *Response
}

func (ResponseCompleted) event() {}

// ResponseIncomplete is response.incomplete: the model stopped before finishing, a terminal
// event.
type ResponseIncomplete struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// Response is the response as it stopped, its status StatusIncomplete and its
	// IncompleteDetails saying why, with its output and usage.
	Response *Response
}

func (ResponseIncomplete) event() {}

// ResponseFailed is response.failed: the response failed, a terminal event.
type ResponseFailed struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// Response is the response as it failed, its status StatusFailed and its Error the
	// failure, with the output made before it: each item done as it was done, and an item
	// still open with the content, or the arguments, made so far, in StatusInProgress.
	Response *Response
}

func (ResponseFailed) event() {}

// ErrorEvent is error: the server reports that the response failed, with the error. The spec
// has response.failed follow it, closing the stream.
type ErrorEvent struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// Error is the error the server reported.
	Error ErrorPayload
}

func (ErrorEvent) event() {}

// ErrorPayload is the error an ErrorEvent carries: the spec's ErrorPayload, without its optional
// headers.
type ErrorPayload struct {
	// Type is the error's type, such as "server_error" or "model_error". The spec requires one;
	// it is empty when the endpoint gave none.
	Type string
	// Code is the error's code, empty when it has none.
	Code string
	// Message is the human-readable error message.
	Message string
	// Param is the request parameter the error concerns, empty when it concerns none.
	Param string
}

// OutputItemAdded is response.output_item.added: an output item was added, before any event
// about its content, its status StatusInProgress when it has one.
type OutputItemAdded struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// Item is the item as far as it is known when it is added: a Message has its role and no
	// content, a FunctionCall its call id and name and no arguments, and a Reasoning item no
	// content and no encrypted content.
	Item Item
}

func (OutputItemAdded) event() {}

// OutputItemDone is response.output_item.done: an output item is finished, the last event
// about it.
type OutputItemDone struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// Item is the whole item, with its status when it has one. A Reasoning item's carries the
	// encrypted content its format writes, so the item can be sent back.
	Item Item
}

func (OutputItemDone) event() {}

// ContentPartAdded is response.content_part.added: a content part was added to an output item,
// empty, ahead of its deltas.
type ContentPartAdded struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the item's id, its ID as added.
	ItemID string
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// ContentIndex is the part's place in the item's content.
	ContentIndex int
	// Part is the part as added: an OutputText or a Refusal of a Message, or a ReasoningText of
	// a Reasoning item, its text empty.
	Part ContentPart
}

func (ContentPartAdded) event() {}

// ContentPartDone is response.content_part.done: a content part is finished, after its text's
// done event.
type ContentPartDone struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the item's id, its ID as added.
	ItemID string
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// ContentIndex is the part's place in the item's content.
	ContentIndex int
	// Part is the whole part.
	Part ContentPart
}

func (ContentPartDone) event() {}

// OutputTextDelta is response.output_text.delta: text was appended to an OutputText part.
type OutputTextDelta struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the item's id, its ID as added.
	ItemID string
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// ContentIndex is the part's place in the item's content.
	ContentIndex int
	// Delta is the text appended.
	Delta string
}

func (OutputTextDelta) event() {}

// OutputTextDone is response.output_text.done: an OutputText part's text is finished.
type OutputTextDone struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the item's id, its ID as added.
	ItemID string
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// ContentIndex is the part's place in the item's content.
	ContentIndex int
	// Text is the part's whole text, its deltas joined.
	Text string
}

func (OutputTextDone) event() {}

// RefusalDelta is response.refusal.delta: refusal text was appended to a Refusal part.
type RefusalDelta struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the item's id, its ID as added.
	ItemID string
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// ContentIndex is the part's place in the item's content.
	ContentIndex int
	// Delta is the refusal text appended.
	Delta string
}

func (RefusalDelta) event() {}

// RefusalDone is response.refusal.done: a Refusal part's text is finished.
type RefusalDone struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the item's id, its ID as added.
	ItemID string
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// ContentIndex is the part's place in the item's content.
	ContentIndex int
	// Refusal is the part's whole refusal text, its deltas joined.
	Refusal string
}

func (RefusalDone) event() {}

// ReasoningDelta is response.reasoning.delta: reasoning text was appended to a ReasoningText
// part of a Reasoning item.
type ReasoningDelta struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the reasoning item's id, its ID as added.
	ItemID string
	// OutputIndex is the reasoning item's place in the response's output.
	OutputIndex int
	// ContentIndex is the part's place in the item's content.
	ContentIndex int
	// Delta is the reasoning text appended.
	Delta string
}

func (ReasoningDelta) event() {}

// ReasoningDone is response.reasoning.done: a ReasoningText part's text is finished.
type ReasoningDone struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the reasoning item's id, its ID as added.
	ItemID string
	// OutputIndex is the reasoning item's place in the response's output.
	OutputIndex int
	// ContentIndex is the part's place in the item's content.
	ContentIndex int
	// Text is the part's whole reasoning text, its deltas joined.
	Text string
}

func (ReasoningDone) event() {}

// FunctionCallArgumentsDelta is response.function_call_arguments.delta: argument text was
// appended to a FunctionCall item. A function call has no content parts, so the event carries
// no content index.
type FunctionCallArgumentsDelta struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the function call's id, its ID as added.
	ItemID string
	// OutputIndex is the function call's place in the response's output.
	OutputIndex int
	// Delta is the argument text appended.
	Delta string
}

func (FunctionCallArgumentsDelta) event() {}

// FunctionCallArgumentsDone is response.function_call_arguments.done: a FunctionCall item's
// arguments are finished, ahead of its output_item.done.
type FunctionCallArgumentsDone struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the function call's id, its ID as added.
	ItemID string
	// OutputIndex is the function call's place in the response's output.
	OutputIndex int
	// Arguments is the call's whole arguments text, its deltas joined.
	Arguments string
}

func (FunctionCallArgumentsDone) event() {}
