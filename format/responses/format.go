package responses

// Options adjusts the format to an endpoint that departs from the spec. Its zero value is the
// spec's form, which an endpoint that follows the spec takes as it is.
type Options struct {
	// ReasoningAsContent sends a reasoning item back with its reasoning text as content, beside
	// its encrypted_content, for an endpoint that reads the reasoning from there, as llama.cpp's
	// router does. The spec's input reasoning item has no content, so the zero value, false,
	// never sends it.
	ReasoningAsContent bool
}

// Format is the Open Responses wire format, a format.Format. Build it with New; it is safe for
// concurrent use.
type Format struct {
	opts Options
}

// New returns the Open Responses format adjusted by opts, for client.Config's Format. The zero
// Options give the spec's form.
func New(opts Options) *Format {
	return &Format{opts: opts}
}

// Route is "responses", the Open Responses endpoint below a base URL such as
// "http://localhost:8080/v1".
func (*Format) Route() string {
	return "responses"
}
