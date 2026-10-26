package protocol

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
)

// Request is one request for a model's response: Open Responses' create-response body, in its
// stateless subset.
type Request struct {
	// Model names the model to respond. It is required.
	Model string
	// Instructions guide the model for this request, ahead of Input.
	Instructions string
	// Input is the conversation so far, oldest item first. It holds at least one item.
	Input []Item
	// Tools are the functions the model may call.
	Tools []FunctionTool
	// ToolChoice controls which of Tools the model calls. Nil leaves it to the endpoint.
	ToolChoice ToolChoice
	// Reasoning configures the model's reasoning. Its zero value leaves it to the endpoint.
	Reasoning ReasoningParam
	// MaxOutputTokens bounds the tokens the model generates, reasoning included. Zero leaves
	// the bound to the endpoint.
	MaxOutputTokens int
	// Native is the native tier: a JSON object whose members are merged into the top level of
	// the wire body as they are, for a feature of the endpoint the model doesn't cover, such
	// as a sampling parameter. It is outside the standard model, so its members mean what the
	// endpoint makes them mean. A member that clashes with one the typed fields set is
	// refused.
	Native jsontext.Value
}

// ReasoningParam configures a reasoning model's reasoning: the spec's reasoning parameter. The
// spec's summary member, which asks for a reasoning summary, is left out: Chat Completions has
// no form for it. It joins with the Open Responses format, whose API has one.
type ReasoningParam struct {
	// Effort is how much the model reasons. Empty leaves it to the endpoint.
	Effort ReasoningEffort
}

// ReasoningEffort is how much a model reasons before it answers. More effort may take longer
// and use more output tokens.
type ReasoningEffort string

// The efforts a ReasoningParam takes.
const (
	// ReasoningEffortNone asks the model not to reason.
	ReasoningEffortNone ReasoningEffort = "none"
	// ReasoningEffortLow asks for little reasoning.
	ReasoningEffortLow ReasoningEffort = "low"
	// ReasoningEffortMedium asks for a moderate amount of reasoning.
	ReasoningEffortMedium ReasoningEffort = "medium"
	// ReasoningEffortHigh asks for thorough reasoning.
	ReasoningEffortHigh ReasoningEffort = "high"
	// ReasoningEffortXHigh asks for the most reasoning the model gives.
	ReasoningEffortXHigh ReasoningEffort = "xhigh"
)

// Validate reports whether the request is one the model can describe, so a caller can check a
// request before sending it; a client validates every request before anything is sent. It
// refuses nil, no model, no input, an item or content part that isn't one of this package's by
// value (a *Message is refused), a message with an unknown role or no content, a part its role
// doesn't take, an image with no URL or an unknown detail, an audio clip with no data or an
// unknown format, a reasoning effort the spec doesn't define, a function_call with no call id or name or with arguments that aren't JSON, a
// function_call_output with no call id, both output forms, or a part other than InputText, a
// tool with no name, a name the spec doesn't allow, or a name another tool has, parameters that
// aren't a JSON object, a tool choice that isn't one of this package's by value, an unknown
// mode, a named function or allowed tool the request doesn't declare, an allowed_tools choice
// with no tools, a negative MaxOutputTokens, and a Native that isn't a JSON object with unique
// member names.
func (r *Request) Validate() error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}
	return nil
}

// validate is Validate without the package's prefix on its error.
func (r *Request) validate() error {
	switch {
	case r == nil:
		return errors.New("no request")
	case r.Model == "":
		return errors.New("request has no model")
	case len(r.Input) == 0:
		return errors.New("request has no input")
	case r.MaxOutputTokens < 0:
		return fmt.Errorf("request's max output tokens %d is negative", r.MaxOutputTokens)
	}
	switch r.Reasoning.Effort {
	case "", ReasoningEffortNone, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh:
	default:
		return fmt.Errorf("request has unknown reasoning effort %q", r.Reasoning.Effort)
	}
	for i, it := range r.Input {
		if err := validateItem(it); err != nil {
			return fmt.Errorf("input %d: %w", i, err)
		}
	}
	declared := make(map[string]bool, len(r.Tools))
	for i, t := range r.Tools {
		if err := validateTool(t, declared); err != nil {
			return fmt.Errorf("tool %d: %w", i, err)
		}
		declared[t.Name] = true
	}
	if r.ToolChoice != nil {
		if err := validateToolChoice(r.ToolChoice, declared); err != nil {
			return fmt.Errorf("tool choice: %w", err)
		}
	}
	if len(r.Native) > 0 && (r.Native.Kind() != jsontext.KindBeginObject || !r.Native.IsValid()) {
		return errors.New("request's native field isn't a JSON object with unique member names")
	}
	return nil
}

// validateItem refuses an item that isn't one of the package's by value, and one the model
// doesn't allow.
func validateItem(it Item) error {
	switch it := it.(type) {
	case Message:
		return validateMessage(it)
	case FunctionCall:
		return validateFunctionCall(it)
	case FunctionCallOutput:
		return validateFunctionCallOutput(it)
	case Reasoning:
		// Every field is optional, and Content, which the spec's input item lacks, is ignored
		// on input rather than refused, so a response's Output goes back as it came.
		return nil
	default:
		return fmt.Errorf("unsupported item %T", it)
	}
}

// validateMessage refuses a message with an unknown role or no content, and a part its role
// doesn't take. The spec gives each role its parts: the user input_text and input_image, the
// system and developer input_text alone, and the assistant output_text and refusal. The audio
// extension, an input part like input_image, is the user's alone too.
func validateMessage(m Message) error {
	switch m.Role {
	case RoleUser, RoleSystem, RoleDeveloper, RoleAssistant:
	default:
		return fmt.Errorf("message has unknown role %q", m.Role)
	}
	if len(m.Content) == 0 {
		return fmt.Errorf("%s message has no content", m.Role)
	}
	for i, p := range m.Content {
		if err := validatePart(m.Role, p); err != nil {
			return fmt.Errorf("%s message content %d: %w", m.Role, i, err)
		}
	}
	return nil
}

// validatePart refuses a part role doesn't take, and an image or audio part that is malformed.
func validatePart(role Role, p ContentPart) error {
	switch p := p.(type) {
	case InputText:
		if role != RoleAssistant {
			return nil
		}
	case InputImage:
		if role == RoleUser {
			return validateImage(p)
		}
	case InputAudio:
		if role == RoleUser {
			return validateAudio(p)
		}
	case OutputText, Refusal:
		if role == RoleAssistant {
			return nil
		}
	}
	return fmt.Errorf("unsupported part %T", p)
}

// validateImage refuses an image with no URL or an unknown detail level.
func validateImage(p InputImage) error {
	if p.ImageURL == "" {
		return errors.New("image has no URL")
	}
	switch p.Detail {
	case "", ImageDetailAuto, ImageDetailLow, ImageDetailHigh:
		return nil
	}
	return fmt.Errorf("image has unknown detail %q", p.Detail)
}

// validateAudio refuses a clip with no data or an unknown format.
func validateAudio(p InputAudio) error {
	if len(p.Data) == 0 {
		return errors.New("audio has no data")
	}
	switch p.Format {
	case AudioFormatWAV, AudioFormatMP3:
		return nil
	}
	return fmt.Errorf("audio has unknown format %q", p.Format)
}

// validateFunctionCall refuses a call with no call id or name, or with arguments that aren't
// JSON.
func validateFunctionCall(c FunctionCall) error {
	switch {
	case c.CallID == "":
		return errors.New("function_call has no call id")
	case c.Name == "":
		return fmt.Errorf("function_call %s has no name", c.CallID)
	case c.Arguments != "" && !jsontext.Value(c.Arguments).IsValid():
		return fmt.Errorf("function_call %s has arguments that aren't JSON", c.CallID)
	}
	return nil
}

// validateFunctionCallOutput refuses an output with no call id, with both a string and parts,
// or with a part the spec doesn't take there. Of the spec's output parts, the stateless subset
// covers input_text.
func validateFunctionCallOutput(o FunctionCallOutput) error {
	switch {
	case o.CallID == "":
		return errors.New("function_call_output has no call id")
	case o.Output != "" && len(o.OutputParts) > 0:
		return fmt.Errorf("function_call_output %s has both an output string and output parts", o.CallID)
	}
	for i, p := range o.OutputParts {
		if _, ok := p.(InputText); !ok {
			return fmt.Errorf("function_call_output %s part %d: unsupported part %T", o.CallID, i, p)
		}
	}
	return nil
}

// validateTool refuses a tool whose name is missing, outside the spec's pattern, or among
// declared, the names of the tools before it, and parameters that aren't a JSON object.
func validateTool(t FunctionTool, declared map[string]bool) error {
	switch {
	case t.Name == "":
		return errors.New("tool has no name")
	case !validToolName(t.Name):
		return fmt.Errorf("tool name %q isn't 1 to 64 letters, digits, underscores, and hyphens", t.Name)
	case declared[t.Name]:
		return fmt.Errorf("tool name %q is declared twice", t.Name)
	case len(t.Parameters) > 0 && (t.Parameters.Kind() != jsontext.KindBeginObject || !t.Parameters.IsValid()):
		return fmt.Errorf("tool %s's parameters aren't a JSON object with unique member names", t.Name)
	}
	return nil
}

// validToolName reports whether name matches the spec's pattern for a function's name,
// ^[a-zA-Z0-9_-]+$, at most 64 characters long.
func validToolName(name string) bool {
	if len(name) > 64 {
		return false
	}
	for _, c := range []byte(name) {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// validateToolChoice refuses a choice that isn't one of the package's by value, an unknown
// mode, a function declared doesn't name, and an allowed_tools choice with no tools.
func validateToolChoice(tc ToolChoice, declared map[string]bool) error {
	switch tc := tc.(type) {
	case ToolChoiceMode:
		return validateToolChoiceMode(tc)
	case FunctionToolChoice:
		return validateFunctionToolChoice(tc, declared)
	case AllowedToolChoice:
		if len(tc.Tools) == 0 {
			return errors.New("allowed_tools has no tools")
		}
		for i, f := range tc.Tools {
			if err := validateFunctionToolChoice(f, declared); err != nil {
				return fmt.Errorf("allowed_tools tool %d: %w", i, err)
			}
		}
		if err := validateToolChoiceMode(tc.Mode); err != nil {
			return fmt.Errorf("allowed_tools: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported tool choice %T", tc)
	}
}

// validateToolChoiceMode refuses a mode other than auto, none, and required.
func validateToolChoiceMode(m ToolChoiceMode) error {
	switch m {
	case ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired:
		return nil
	}
	return fmt.Errorf("unknown mode %q", m)
}

// validateFunctionToolChoice refuses a function declared doesn't name.
func validateFunctionToolChoice(f FunctionToolChoice, declared map[string]bool) error {
	if !declared[f.Name] {
		return fmt.Errorf("function %q isn't one of the request's tools", f.Name)
	}
	return nil
}
