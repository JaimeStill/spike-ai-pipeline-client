package protocol

// Event is one of Open Responses' streaming events: a semantic event of a streamed response,
// under the spec's name. A stream opens with ResponseCreated and closes with one terminal event,
// ResponseCompleted, ResponseIncomplete, or ResponseFailed, each carrying the Response. Between
// them each output item is added, its content parts are added, streamed as deltas, and done, and
// the item is done, in that order; an Accumulator folds the events back into the Response. The
// set is closed: the event types are this package's, and an event is one of them by value, such
// as OutputTextDelta, never a pointer to one.
//
// Every event carries its SequenceNumber, which increases through the stream. An event about an
// output item carries the item's OutputIndex, its place in the Response's Output, and an event
// about a content part carries the item's ItemID, empty when the format supplies no item ids,
// and the part's ContentIndex, its place in the item's content.
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
	// Response is the response as it failed, with the output made before the failure.
	Response *Response
}

func (ResponseFailed) event() {}

// OutputItemAdded is response.output_item.added: an output item was added, its status
// StatusInProgress, before any event about its content.
type OutputItemAdded struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// Item is the item as far as it is known when it is added: a Message has its role and no
	// content.
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
	// Item is the whole item, with its status.
	Item Item
}

func (OutputItemDone) event() {}

// ContentPartAdded is response.content_part.added: a content part was added to an output item,
// empty, ahead of its deltas.
type ContentPartAdded struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the item's id, empty when the format supplies none.
	ItemID string
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// ContentIndex is the part's place in the item's content.
	ContentIndex int
	// Part is the part as added: an OutputText or a Refusal, its text empty.
	Part ContentPart
}

func (ContentPartAdded) event() {}

// ContentPartDone is response.content_part.done: a content part is finished, after its text's
// done event.
type ContentPartDone struct {
	// SequenceNumber is the event's place in the stream.
	SequenceNumber int
	// ItemID is the item's id, empty when the format supplies none.
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
	// ItemID is the item's id, empty when the format supplies none.
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
	// ItemID is the item's id, empty when the format supplies none.
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
	// ItemID is the item's id, empty when the format supplies none.
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
	// ItemID is the item's id, empty when the format supplies none.
	ItemID string
	// OutputIndex is the item's place in the response's output.
	OutputIndex int
	// ContentIndex is the part's place in the item's content.
	ContentIndex int
	// Refusal is the part's whole refusal text, its deltas joined.
	Refusal string
}

func (RefusalDone) event() {}
