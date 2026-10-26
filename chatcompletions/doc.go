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
//
// # Folding
//
// The request's instructions become a leading system message, and each message item a message
// of its role, its content a plain string when it is a single text part and an array of text
// and refusal parts otherwise. max_output_tokens goes out as max_completion_tokens. The
// request's Native members, the native tier, are merged into the top level of the body, and a
// member that clashes with one the request's typed fields set is refused.
//
// # Unfolding
//
// The reply's first choice gives an assistant message item, with an output_text part for its
// content and a refusal part for its refusal. finish_reason gives the response's status:
//
//	stop, tool_calls  completed
//	length            incomplete, reason max_output_tokens
//	content_filter    incomplete, reason content_filter
//
// The reply's usage gives the response's: prompt_tokens, completion_tokens, total_tokens, and
// completion_tokens_details.reasoning_tokens.
package chatcompletions
