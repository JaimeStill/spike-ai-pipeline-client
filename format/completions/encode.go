package completions

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/JaimeStill/spike-ai-pipeline-client/internal/native"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// EncodeRequest encodes req, which the client has validated, as a Chat Completions request
// body. Instructions become a leading system message, and the input items messages:
//
//	message               a message of its role
//	assistant run         one assistant message: reasoning_content from the reasoning items
//	                      that open the run, content from its assistant message, tool_calls
//	                      from the function_calls after it, each call's id its call_id; a
//	                      run of calls alone has no content
//	function_call_output  a tool message, tool_call_id from its call_id
//
// An assistant run opens at a reasoning item, at an assistant message no reasoning item opened,
// or at a function_call no assistant message or reasoning item opened, and takes the
// function_calls that follow it. Reasoning leads to what follows it, so a reasoning item always
// opens a new run, joined by the reasoning items right after it; their texts go out
// concatenated, in order, as reasoning_content.
//
// A reasoning item goes out through its encrypted_content alone: the envelope DecodeResponse
// wrote there, which holds the reply's reasoning_content. Its content, which the spec's input
// reasoning item lacks, is never sent. A reasoning item whose encrypted_content isn't this
// format's envelope, being empty, another format's, another version's, or malformed, is
// dropped: it is never sent, and it is never an error, so a conversation can carry another
// format's reasoning through this one. So is one whose envelope holds no reasoning, and a run of
// reasoning that no assistant message or function_call follows, since an assistant message
// needs content or tool_calls, as llama.cpp b11669 requires.
//
// A message's content, and a tool message's, is a plain string when it is a single input_text
// or output_text part and an array of parts otherwise: text, refusal, image_url for an
// input_image (its URL and, when set, detail), and input_audio for the audio extension (its
// bytes in base64, and format). A call with empty arguments goes out with "{}", a call of no
// arguments, since Chat Completions' arguments are always JSON text.
//
// Tools go out as tools of type function, and the tool choice as tool_choice: a mode as its
// string, a named function as {"type":"function","function":{"name"}}, and an allowed set as
// {"type":"allowed_tools","allowed_tools":{"mode","tools"}}, its tools named the same way. An
// allowed set of mode none has no Chat Completions form, whose allowed_tools modes are auto and
// required, and is refused. Reasoning's effort goes out as reasoning_effort, and MaxOutputTokens
// as max_completion_tokens.
//
// The hints Chat Completions has no form for are dropped, never refused: reasoning's summary.
//
// Text's format goes out as response_format: plain text as {"type":"text"}, sent because the
// caller set it, and a JSON schema as {"type":"json_schema","json_schema":{"name","schema"}},
// with description and strict when set. Text's verbosity goes out as verbosity.
// The request is unstreamed, sent with stream false; EncodeStreamRequest streams it.
// Native's members, the native tier, are merged into the top level, and a member named like one
// the typed fields set is refused, as are stream, which is always set, and stream_options,
// which the streamed request sets. It adds no headers.
func (*Format) EncodeRequest(req *protocol.Request, _ http.Header) ([]byte, error) {
	return encodeRequest(req, false)
}

// EncodeStreamRequest encodes req as EncodeRequest does, as a streamed request: with stream
// true, and stream_options.include_usage true, which asks for the usage the stream's terminal
// event carries. A Native stream or stream_options member is refused, as EncodeRequest refuses
// them. It adds no headers.
func (*Format) EncodeStreamRequest(req *protocol.Request, _ http.Header) ([]byte, error) {
	return encodeRequest(req, true)
}

// encodeRequest encodes req as EncodeRequest describes, streamed when stream is set.
func encodeRequest(req *protocol.Request, stream bool) ([]byte, error) {
	var err error
	body := request{
		Model:               req.Model,
		ReasoningEffort:     string(req.Reasoning.Effort),
		MaxCompletionTokens: req.MaxOutputTokens,
		Verbosity:           string(req.Text.Verbosity),
		Stream:              stream,
		Native:              req.Native,
	}
	if stream {
		body.StreamOptions = &streamOptions{IncludeUsage: true}
	} else if native.Has(req.Native, "stream_options") {
		// The unstreamed body has no stream_options for the member to clash with, but it is
		// the stream's, so it is refused in both.
		return nil, errors.New("completions: native member stream_options clashes with the format's streaming")
	}
	if req.Text.Format != nil {
		if body.ResponseFormat, err = encodeTextFormat(req.Text.Format); err != nil {
			return nil, fmt.Errorf("completions: text format: %w", err)
		}
	}
	if req.Instructions != "" {
		content, err := json.Marshal(req.Instructions)
		if err != nil {
			return nil, fmt.Errorf("completions: instructions: %w", err)
		}
		body.Messages = append(body.Messages, message{Role: string(protocol.RoleSystem), Content: content})
	}
	messages, err := encodeInput(req.Input)
	if err != nil {
		return nil, fmt.Errorf("completions: %w", err)
	}
	body.Messages = append(body.Messages, messages...)
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, tool{Type: "function", Function: functionTool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
			Strict:      t.Strict,
		}})
	}
	if req.ToolChoice != nil {
		if body.ToolChoice, err = encodeToolChoice(req.ToolChoice); err != nil {
			return nil, fmt.Errorf("completions: tool choice: %w", err)
		}
	}
	b, err := native.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("completions: %w", err)
	}
	return b, nil
}

// encodeInput encodes the input items as messages, joining each run of assistant items into one
// assistant message, as EncodeRequest describes.
func encodeInput(items []protocol.Item) ([]message, error) {
	var messages []message
	// run is the index in messages of the open run's assistant message, or -1 when no run is
	// open or the open run is reasoning alone so far. reasoning is that reasoning, the
	// reasoning_content of the assistant message that joins the run, empty when there is none.
	run, reasoning := -1, ""
	for i, it := range items {
		switch it := it.(type) {
		case protocol.Reasoning:
			text, ok := openEnvelope(it.EncryptedContent)
			if !ok || text == "" {
				continue
			}
			run = -1
			reasoning += text
		case protocol.Message:
			content, err := encodeContent(it.Content)
			if err != nil {
				return nil, fmt.Errorf("input %d: %w", i, err)
			}
			m := message{Role: string(it.Role), Content: content}
			run = -1
			if it.Role == protocol.RoleAssistant {
				m.ReasoningContent = reasoning
				run = len(messages)
			}
			messages = append(messages, m)
			reasoning = ""
		case protocol.FunctionCall:
			if run < 0 {
				messages = append(messages, message{Role: string(protocol.RoleAssistant), ReasoningContent: reasoning})
				run, reasoning = len(messages)-1, ""
			}
			args := it.Arguments
			if args == "" {
				args = "{}"
			}
			messages[run].ToolCalls = append(messages[run].ToolCalls, toolCall{
				ID:       it.CallID,
				Type:     "function",
				Function: functionCall{Name: it.Name, Arguments: args},
			})
		case protocol.FunctionCallOutput:
			content, err := encodeOutput(it)
			if err != nil {
				return nil, fmt.Errorf("input %d: %w", i, err)
			}
			messages = append(messages, message{Role: "tool", Content: content, ToolCallID: it.CallID})
			run, reasoning = -1, ""
		default:
			return nil, fmt.Errorf("input %d: item %T has no Chat Completions form", i, it)
		}
	}
	return messages, nil
}

// openEnvelope returns the reasoning_content the envelope sealed in encryptedContent holds, and
// whether encryptedContent is this format's envelope at all: false for one that is empty,
// another format's or version's, or malformed.
func openEnvelope(encryptedContent string) (string, bool) {
	rest, ok := strings.CutPrefix(encryptedContent, envelopePrefix)
	if !ok {
		return "", false
	}
	b, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return "", false
	}
	var e envelope
	if json.Unmarshal(b, &e) != nil {
		return "", false
	}
	return e.ReasoningContent, true
}

// encodeOutput encodes a function_call_output's result as a tool message's content: its output
// string, or its parts as a message's content.
func encodeOutput(o protocol.FunctionCallOutput) (jsontext.Value, error) {
	if len(o.OutputParts) > 0 {
		return encodeContent(o.OutputParts)
	}
	return json.Marshal(o.Output)
}

// encodeToolChoice encodes a tool choice as tool_choice.
func encodeToolChoice(tc protocol.ToolChoice) (jsontext.Value, error) {
	switch tc := tc.(type) {
	case protocol.ToolChoiceMode:
		return json.Marshal(string(tc))
	case protocol.FunctionToolChoice:
		return json.Marshal(namedChoice(tc))
	case protocol.AllowedToolChoice:
		if tc.Mode == protocol.ToolChoiceNone {
			return nil, errors.New("allowed_tools of mode none has no Chat Completions form")
		}
		allowed := allowedToolsChoice{
			Type:         "allowed_tools",
			AllowedTools: allowedTools{Mode: string(tc.Mode)},
		}
		for _, f := range tc.Tools {
			allowed.AllowedTools.Tools = append(allowed.AllowedTools.Tools, namedChoice(f))
		}
		return json.Marshal(allowed)
	default:
		return nil, fmt.Errorf("tool choice %T has no Chat Completions form", tc)
	}
}

// encodeTextFormat encodes a text format as response_format.
func encodeTextFormat(f protocol.TextFormat) (*responseFormat, error) {
	switch f := f.(type) {
	case protocol.PlainTextFormat:
		return &responseFormat{Type: "text"}, nil
	case protocol.JSONSchemaFormat:
		return &responseFormat{Type: "json_schema", JSONSchema: &jsonSchema{
			Name:        f.Name,
			Description: f.Description,
			Schema:      f.Schema,
			Strict:      f.Strict,
		}}, nil
	default:
		return nil, fmt.Errorf("text format %T has no Chat Completions form", f)
	}
}

// namedChoice is f as a Chat Completions named tool choice.
func namedChoice(f protocol.FunctionToolChoice) namedToolChoice {
	return namedToolChoice{Type: "function", Function: functionName{Name: f.Name}}
}

// encodeContent encodes a message's content: a single text part as a plain string, anything
// else, such as a message with an image or audio part, as an array of parts.
func encodeContent(parts []protocol.ContentPart) (jsontext.Value, error) {
	if len(parts) == 1 {
		switch p := parts[0].(type) {
		case protocol.InputText:
			return json.Marshal(p.Text)
		case protocol.OutputText:
			return json.Marshal(p.Text)
		}
	}
	wire := make([]jsontext.Value, 0, len(parts))
	for i, p := range parts {
		var b jsontext.Value
		var err error
		switch p := p.(type) {
		case protocol.InputText:
			b, err = json.Marshal(textPart{Type: "text", Text: p.Text})
		case protocol.OutputText:
			b, err = json.Marshal(textPart{Type: "text", Text: p.Text})
		case protocol.InputImage:
			b, err = json.Marshal(imagePart{
				Type:     "image_url",
				ImageURL: imageURL{URL: p.ImageURL, Detail: string(p.Detail)},
			})
		case protocol.InputAudio:
			b, err = json.Marshal(audioPart{
				Type: "input_audio",
				InputAudio: inputAudio{
					Data:   base64.StdEncoding.EncodeToString(p.Data),
					Format: string(p.Format),
				},
			})
		case protocol.Refusal:
			b, err = json.Marshal(refusalPart{Type: "refusal", Refusal: p.Refusal})
		default:
			return nil, fmt.Errorf("content %d: part %T has no Chat Completions form", i, p)
		}
		if err != nil {
			return nil, fmt.Errorf("content %d: %w", i, err)
		}
		wire = append(wire, b)
	}
	return json.Marshal(wire)
}
