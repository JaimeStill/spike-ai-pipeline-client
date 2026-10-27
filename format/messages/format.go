package messages

// version is the Messages API version the format speaks, sent with every request as its
// anthropic-version header.
const version = "2026-10-21"

// Format is the Anthropic Messages wire format, a format.Format. Build it with New; it is safe
// for concurrent use.
type Format struct{}

// New returns the Anthropic Messages format, for client.Config's Format.
func New() *Format {
	return &Format{}
}

// Route is "messages", the Messages endpoint below a base URL such as
// "https://api.anthropic.com/v1" or "http://localhost:8080/v1".
func (*Format) Route() string {
	return "messages"
}
