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

// Validate reports whether the request is one the model can describe, so a caller can check a
// request before sending it; a client validates every request before anything is sent. It
// refuses nil, no model, no input, an item or content part that isn't one of this package's by
// value (a *Message is refused), a message with an unknown role or no content, a part its role
// doesn't take, an image with no URL or an unknown detail, an audio clip with no data or an
// unknown format, a negative MaxOutputTokens, and a Native that isn't a JSON object with unique
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
