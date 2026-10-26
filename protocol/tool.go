package protocol

import "encoding/json/jsontext"

// FunctionTool is a function the model may call: the spec's function tool, its one tool kind,
// so a request's Tools are FunctionTools rather than a closed set of kinds. The model calls it
// with a FunctionCall item, and the caller answers with a FunctionCallOutput.
type FunctionTool struct {
	// Name names the function: 1 to 64 letters, digits, underscores, and hyphens, unique among
	// the request's tools. It is required.
	Name string
	// Description tells the model what the function does and when to call it. Empty sends none.
	Description string
	// Parameters is a JSON Schema object describing the function's arguments. Empty sends none.
	Parameters jsontext.Value
	// Strict asks the endpoint to hold the model's arguments to Parameters exactly. False sends
	// nothing, leaving it to the endpoint's default.
	Strict bool
}

// ToolChoice controls which tool, if any, the model calls: a ToolChoiceMode, a
// FunctionToolChoice naming one function, or an AllowedToolChoice narrowing the tools to a
// set. A nil ToolChoice leaves the choice to the endpoint, whose default is ToolChoiceAuto
// when the request has tools. The set is closed: the choices are this package's, by value.
type ToolChoice interface {
	toolChoice()
}

// ToolChoiceMode is how the model chooses among the tools: a ToolChoice of its own, and the
// mode of an AllowedToolChoice.
type ToolChoiceMode string

// The modes a ToolChoice takes.
const (
	// ToolChoiceAuto lets the model choose whether to call tools and which.
	ToolChoiceAuto ToolChoiceMode = "auto"
	// ToolChoiceNone stops the model calling any tool.
	ToolChoiceNone ToolChoiceMode = "none"
	// ToolChoiceRequired has the model call at least one tool.
	ToolChoiceRequired ToolChoiceMode = "required"
)

func (ToolChoiceMode) toolChoice() {}

// FunctionToolChoice has the model call one function, which the request declares among its
// Tools. It is also one of the tools an AllowedToolChoice allows.
type FunctionToolChoice struct {
	// Name is the function's name. It is required.
	Name string
}

func (FunctionToolChoice) toolChoice() {}

// AllowedToolChoice narrows the tools the model may call to a set of the request's Tools, and
// says how it chooses among them.
type AllowedToolChoice struct {
	// Tools are the functions the model may call, at least one, each declared among the
	// request's Tools.
	Tools []FunctionToolChoice
	// Mode is how the model chooses among Tools. It is required.
	Mode ToolChoiceMode
}

func (AllowedToolChoice) toolChoice() {}
