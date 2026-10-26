package client

// Item is one entry of a request's input or a response's output, as Open Responses defines
// items. The set is closed: the item types are this package's, and an item is one of them by
// value, such as Message, never a pointer to one.
type Item interface {
	item()
}

// Message is a message item: content from a role. Input messages from the user, system, or
// developer hold InputText parts; assistant messages hold OutputText and Refusal parts.
type Message struct {
	// ID is the item's id, when the format supplies one. Chat Completions doesn't.
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

// ContentPart is one part of a message's content. The set is closed: the part types are this
// package's, and a part is one of them by value, such as InputText.
type ContentPart interface {
	contentPart()
}

// InputText is text input to the model: input_text.
type InputText struct {
	// Text is the input.
	Text string
}

func (InputText) contentPart() {}

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

// Status is the status of a response or of an item in its output.
type Status string

// The statuses a stateless response and its items take.
const (
	// StatusCompleted is a response or item the model finished.
	StatusCompleted Status = "completed"
	// StatusIncomplete is a response or item the model stopped before finishing; a response's
	// IncompleteDetails says why.
	StatusIncomplete Status = "incomplete"
)
