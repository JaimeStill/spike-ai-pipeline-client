package completions

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// EncodeRequest encodes req, which the client has validated, as a Chat Completions request
// body. Instructions become a leading system message, and the input items messages:
//
//	message               a message of its role
//	assistant run         one assistant message: content from the assistant message that
//	                      opens the run, tool_calls from the function_calls after it, each
//	                      call's id its call_id; a run of calls alone has no content
//	function_call_output  a tool message, tool_call_id from its call_id
//
// An assistant run opens at an assistant message, or at a function_call no assistant message
// opened, and takes the function_calls that follow it.
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
// required, and is refused. MaxOutputTokens goes out as max_completion_tokens.
// Native's members, the native tier, are merged into the top level, and a member named like one
// the typed fields set is refused. It adds no headers.
func (*Format) EncodeRequest(req *protocol.Request, _ http.Header) ([]byte, error) {
	body := request{
		Model:               req.Model,
		MaxCompletionTokens: req.MaxOutputTokens,
		Native:              req.Native,
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
	b, err := json.Marshal(body)
	if err != nil {
		var se *jsontext.SyntacticError
		if errors.As(err, &se) && errors.Is(se.Err, jsontext.ErrDuplicateName) {
			return nil, fmt.Errorf("completions: native member %s clashes with a member the request sets",
				se.JSONPointer.LastToken())
		}
		return nil, fmt.Errorf("completions: encode request: %w", err)
	}
	return b, nil
}

// encodeInput encodes the input items as messages, folding each run of assistant items into one
// assistant message. A run opens at an assistant message, or at a function_call no assistant
// message opened, takes the function_calls that follow, and closes at any other item.
func encodeInput(items []protocol.Item) ([]message, error) {
	var messages []message
	// run is the index in messages of the open run's assistant message, or -1 when no run is
	// open. The reasoning items that precede an assistant turn are the run's to fold in too,
	// as reasoning_content, once the format covers them.
	run := -1
	for i, it := range items {
		switch it := it.(type) {
		case protocol.Message:
			content, err := encodeContent(it.Content)
			if err != nil {
				return nil, fmt.Errorf("input %d: %w", i, err)
			}
			messages = append(messages, message{Role: string(it.Role), Content: content})
			run = -1
			if it.Role == protocol.RoleAssistant {
				run = len(messages) - 1
			}
		case protocol.FunctionCall:
			if run < 0 {
				messages = append(messages, message{Role: string(protocol.RoleAssistant)})
				run = len(messages) - 1
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
			run = -1
		default:
			return nil, fmt.Errorf("input %d: item %T has no Chat Completions form", i, it)
		}
	}
	return messages, nil
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
