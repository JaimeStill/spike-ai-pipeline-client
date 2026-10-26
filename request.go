package client

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

// validate refuses a request the model can't describe: nil, no model, no input, an item or
// content part that isn't one of the package's by value, a message with an unknown role or no
// content, a part its role doesn't take, a negative MaxOutputTokens, or a Native that isn't a
// JSON object with unique member names.
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
	for i, it := range r.Input {
		if err := validateItem(it); err != nil {
			return fmt.Errorf("input %d: %w", i, err)
		}
	}
	if len(r.Native) > 0 && (r.Native.Kind() != jsontext.KindBeginObject || !r.Native.IsValid()) {
		return errors.New("request's native field isn't a JSON object with unique member names")
	}
	return nil
}

// validateItem refuses an item that isn't one of the package's by value, and a message whose
// role or content the model doesn't allow.
func validateItem(it Item) error {
	switch it := it.(type) {
	case Message:
		return validateMessage(it)
	default:
		return fmt.Errorf("unsupported item %T", it)
	}
}

// validateMessage refuses a message with an unknown role or no content, and a part its role
// doesn't take: input parts from the user, system, and developer, output parts from the
// assistant.
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
		var ok bool
		switch p.(type) {
		case InputText:
			ok = m.Role != RoleAssistant
		case OutputText, Refusal:
			ok = m.Role == RoleAssistant
		}
		if !ok {
			return fmt.Errorf("%s message content %d: unsupported part %T", m.Role, i, p)
		}
	}
	return nil
}
