package protocol

import "encoding/json/jsontext"

// TextParam configures the model's text output: the spec's text parameter, its format and its
// verbosity.
type TextParam struct {
	// Format is the form the model's output_text takes. Nil leaves it to the endpoint, whose
	// default is plain text.
	Format TextFormat
	// Verbosity is how much detail the model's text gives. Empty leaves it to the endpoint.
	Verbosity Verbosity
}

// TextFormat is the form of the model's output_text: a PlainTextFormat or a JSONSchemaFormat,
// the spec's text formats for a request. The set is closed: the formats are this package's, by
// value. The spec's json_object format is a response's alone, so a request has no form for it.
type TextFormat interface {
	textFormat()
}

// PlainTextFormat has the model answer in plain text: the spec's text format, type text. It is
// the endpoint's default, set explicitly.
type PlainTextFormat struct{}

func (PlainTextFormat) textFormat() {}

// JSONSchemaFormat has the model answer with JSON that a schema describes: the spec's
// json_schema text format, Structured Outputs. The response's output_text is the JSON the model
// produced, as text: the caller decodes it, into the Go value of its choice. The package adds no
// decoding of its own, which would only wrap the caller's json.Unmarshal.
type JSONSchemaFormat struct {
	// Name names the format: 1 to 64 letters, digits, underscores, and hyphens. It is required.
	Name string
	// Description tells the model what the format is for and how to answer in it. Empty sends
	// none.
	Description string
	// Schema is the JSON Schema object the output follows. It is required.
	Schema jsontext.Value
	// Strict asks the endpoint to hold the output to Schema exactly, which supports a subset of
	// JSON Schema. False sends nothing, leaving it to the endpoint's default, which is false.
	Strict bool
}

func (JSONSchemaFormat) textFormat() {}

// Verbosity is how much detail a model's text gives.
type Verbosity string

// The verbosities a TextParam takes.
const (
	// VerbosityLow asks for concise text.
	VerbosityLow Verbosity = "low"
	// VerbosityMedium asks for the model's default detail.
	VerbosityMedium Verbosity = "medium"
	// VerbosityHigh asks for detailed text.
	VerbosityHigh Verbosity = "high"
)
