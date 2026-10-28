package responses

import "encoding/json/jsontext"

// include is every request's include: the reasoning items' encrypted_content, which a stateless
// request needs to send the reasoning back, as the response isn't stored.
var include = []string{"reasoning.encrypted_content"}

// request is the create-response body.
type request struct {
	Model string `json:"model"`
	// Instructions carries instructions, omitted when there are none.
	Instructions string `json:"instructions,omitzero"`
	// Input is the input items, always an array of items, never the string form.
	Input []any `json:"input"`
	// Text carries the text format and verbosity, omitted when the request sets neither.
	Text *textParam `json:"text,omitzero"`
	// MaxOutputTokens carries max_output_tokens, omitted when the request leaves the bound to
	// the endpoint.
	MaxOutputTokens int `json:"max_output_tokens,omitzero"`
	// Stream is always sent, false for an unstreamed request, so a Native stream member clashes
	// with it.
	Stream bool `json:"stream"`
	// Store is always sent, and always false: the client is stateless, and the endpoint keeps
	// nothing for a later request to name. A Native store member clashes with it.
	Store bool `json:"store"`
	// Include is always sent, as include, so a Native include member clashes with it.
	Include []string `json:"include"`
	// Native is the request's native tier, its members merged into the top level. A member
	// named like one the fields above set is a duplicate name, which encoding refuses.
	Native jsontext.Value `json:",embed"`
}

// textParam is the spec's text parameter: a format and a verbosity, each omitted when unset.
type textParam struct {
	Format    *textFormat `json:"format,omitzero"`
	Verbosity string      `json:"verbosity,omitzero"`
}

// textFormat is a text format: of type text, or of type json_schema with its name, description,
// schema, and strict flag, the description and the flag omitted when unset.
type textFormat struct {
	Type        string         `json:"type"`
	Name        string         `json:"name,omitzero"`
	Description string         `json:"description,omitzero"`
	Schema      jsontext.Value `json:"schema,omitzero"`
	Strict      bool           `json:"strict,omitzero"`
}

// messageItem is a message input item: its id, sent when the item has one, its role, and its
// content, always an array of parts.
type messageItem struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitzero"`
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

// textPart is an input_text or output_text content part.
type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// imagePart is an input_image content part: its URL, and its detail, omitted when unset.
type imagePart struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url"`
	Detail   string `json:"detail,omitzero"`
}

// refusalPart is a refusal content part.
type refusalPart struct {
	Type    string `json:"type"`
	Refusal string `json:"refusal"`
}

// audioType is the wire type of the audio input extension, prefixed with the implementor slug
// under the spec's extension rule.
const audioType = "spike-ai-pipeline:input_audio"

// audioPart is the audio input extension's content part: the clip's bytes in standard, padded
// base64, and its format, wav or mp3.
type audioPart struct {
	Type   string `json:"type"`
	Data   string `json:"data"`
	Format string `json:"format"`
}

// response is the part of a response resource the format reads. Members it doesn't read, such
// as model and created_at, are ignored.
type response struct {
	Status            string             `json:"status"`
	Output            []outputItem       `json:"output"`
	IncompleteDetails *incompleteDetails `json:"incomplete_details"`
	Error             *responseError     `json:"error"`
	Usage             usage              `json:"usage"`
}

// outputItem is the part of an output item the format reads: its type, id, and status; a
// message's role and content; a reasoning item's content, summary, and encrypted_content; and a
// function_call's call_id, name, and arguments.
type outputItem struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	Status           string `json:"status"`
	Role             string `json:"role"`
	Content          []part `json:"content"`
	Summary          []part `json:"summary"`
	EncryptedContent string `json:"encrypted_content"`
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
}

// part is the part of an output content part the format reads: its type, and an output_text's,
// reasoning_text's, or summary_text's text, or a refusal's refusal. Annotations and logprobs are
// ignored.
type part struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

// incompleteDetails is a response's incomplete_details: why it is incomplete.
type incompleteDetails struct {
	Reason string `json:"reason"`
}

// responseError is a failed response's error: its code and message.
type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// usage is a response's token counts.
type usage struct {
	InputTokens         int                 `json:"input_tokens"`
	OutputTokens        int                 `json:"output_tokens"`
	TotalTokens         int                 `json:"total_tokens"`
	OutputTokensDetails outputTokensDetails `json:"output_tokens_details"`
}

// outputTokensDetails breaks a response's output tokens down.
type outputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}
