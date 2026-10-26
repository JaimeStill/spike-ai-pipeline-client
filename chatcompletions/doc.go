// Package chatcompletions is the Chat Completions wire format: the adapter that folds the
// client's request model into an OpenAI-compatible chat/completions request and unfolds the
// reply into its response model. A caller selects it by constructor at the composition root,
// as client.Config's Format.
//
// Its exported identifiers:
//
//   - [Format] is the format, a client.Format.
//   - [New] returns the format.
//   - [Format.Route] is the route it posts to, chat/completions.
//   - [Format.EncodeRequest] folds a request into a Chat Completions body.
//   - [Format.DecodeResponse] unfolds a Chat Completions reply into a response.
//   - [Format.DecodeError] reads an OpenAI-style error body.
package chatcompletions
