// Package format is the contract a wire format meets: [Format] converts the protocol package's
// request and response model to and from one API's wire shape. It imports only protocol; each
// implementation is a package below it, named for the endpoint it targets, such as completions
// for Chat Completions, and none imports the client.
//
// It holds the contract and what the formats share. The formats share nothing yet: the
// OpenAI-style error body is decoded in completions, its one user, until a second format reads
// it too.
//
// Its exported identifiers:
//
//   - [Format] is a wire format, the adapter between the protocol and one API's shape.
package format
