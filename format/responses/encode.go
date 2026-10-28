package responses

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"strings"

	"github.com/JaimeStill/spike-ai-pipeline-client/internal/native"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// EncodeRequest encodes req, which the client has validated, as a create-response body under the
// spec's names: model; instructions, omitted when there are none; input, always an array of
// items; tools and tool_choice; reasoning, its effort and summary, omitted when the request sets
// neither; text, its format and verbosity, omitted when the request sets neither; and
// max_output_tokens, omitted when it is zero. It adds no header.
//
// Every request is stateless and unstreamed: it carries stream false, store false, so the
// endpoint keeps nothing, and include ["reasoning.encrypted_content"], so the reasoning comes
// back in a form the next request can send; previous_response_id is never sent.
//
// The input items go out in order, each as an item of its own type:
//
//	message               a message item of its role, its id when it has one, and its content
//	                      as an array of parts
//	function_call         call_id, name, and arguments, and its id when it has one
//	function_call_output  call_id and its output: the output string, or its parts as an array
//	                      of input_text parts; and its id when it has one
//	reasoning             as the format's Options say, below
//
// A call with empty arguments, a call of none, goes out with "{}", as the spec's arguments are
// JSON text. A message's parts are input_text, input_image with its image_url and its detail when set,
// output_text, and refusal. An item's status, ignored on input, is never sent. An input_audio
// part goes out as the client extension, {"type":"spike-ai-pipeline:input_audio","data",
// "format"}, its data the clip's bytes in standard base64 and its format wav or mp3.
//
// A reasoning item goes out through the envelope DecodeResponse wrote in its encrypted_content,
// which holds the server's encrypted_content and the reply's reasoning texts; the item's Content
// is never read. In the spec's form, the zero Options, it goes out as a reasoning item of its id
// when it has one, its summary's summary_text parts, an empty array when it has none, as the
// spec requires, and the server's encrypted_content, and never content, wherever it stands in
// the input. It is dropped, never sent and never an error, when its encrypted_content isn't
// this format's envelope, being empty, another format's, another version's, or malformed, so a
// conversation can carry another format's reasoning through this one; and when the envelope's
// server encrypted_content is empty, as llama.cpp's router's always is, since the item would
// carry no reasoning.
//
// With Options.ResendReasoningText, a reasoning item goes out with content too, a reasoning_text
// part of each of the envelope's texts, and with encrypted_content only when the server's isn't
// empty. It is dropped when its encrypted_content isn't this format's envelope, or when the
// envelope holds neither text nor server encrypted_content. So is a run of reasoning that no
// assistant message or function_call follows, as Chat Completions and Messages drop it:
// llama.cpp's router makes an assistant message of the reasoning, which the reasoning alone
// leaves without content, and refuses it.
//
// Tools go out as tools of type function: name, description and parameters when set, and strict
// always, so false stays false at an endpoint whose default is true, as llama.cpp's router's is.
// The tool choice goes out as tool_choice: a mode as its string, auto, none, or required; a
// named function as {"type":"function","name"}; and an allowed set as
// {"type":"allowed_tools","tools","mode"}, its tools named the same way. Reasoning's effort and
// summary go out as reasoning's effort and summary.
//
// Native's members, the native tier, are merged into the top level, and a member named like one
// the typed fields set is refused, as are stream, store, and include, which are always set.
// previous_response_id and background are refused too, as the package documentation explains.
func (f *Format) EncodeRequest(req *protocol.Request, _ http.Header) ([]byte, error) {
	return f.encode(req, false)
}

// EncodeStreamRequest encodes req as EncodeRequest does, as a streamed request: with stream
// true, which asks for the reply as server-sent events. It refuses what EncodeRequest refuses,
// and a Native stream member, which clashes with the stream member the body always sets.
func (f *Format) EncodeStreamRequest(req *protocol.Request, _ http.Header) ([]byte, error) {
	return f.encode(req, true)
}

// encode encodes req, streamed when stream is set, prefixing an error with the format's name.
func (f *Format) encode(req *protocol.Request, stream bool) ([]byte, error) {
	b, err := f.encodeRequest(req, stream)
	if err != nil {
		return nil, fmt.Errorf("responses: %w", err)
	}
	return b, nil
}

// encodeRequest encodes req as EncodeRequest describes, streamed when stream is set.
func (f *Format) encodeRequest(req *protocol.Request, stream bool) ([]byte, error) {
	if err := refuseStateful(req.Native); err != nil {
		return nil, err
	}
	input, err := f.encodeInput(req.Input)
	if err != nil {
		return nil, err
	}
	text, err := encodeText(req.Text)
	if err != nil {
		return nil, err
	}
	choice, err := encodeToolChoice(req.ToolChoice)
	if err != nil {
		return nil, err
	}
	body := request{
		Model:           req.Model,
		Instructions:    req.Instructions,
		Input:           input,
		ToolChoice:      choice,
		Text:            text,
		MaxOutputTokens: req.MaxOutputTokens,
		Stream:          stream,
		Store:           false,
		Include:         include,
		Native:          req.Native,
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, functionTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
			Strict:      t.Strict,
		})
	}
	if r := req.Reasoning; r != (protocol.ReasoningParam{}) {
		body.Reasoning = &reasoningParam{Effort: string(r.Effort), Summary: string(r.Summary)}
	}
	return native.Marshal(body)
}

// refuseStateful refuses a Native, which the client has validated, that has a
// previous_response_id or background member, each of which asks the endpoint to keep state, as
// the package documentation explains.
func refuseStateful(v jsontext.Value) error {
	for _, name := range []string{"previous_response_id", "background"} {
		if native.Has(v, name) {
			return fmt.Errorf("native member %s asks the endpoint to keep state, which a stateless request never does", name)
		}
	}
	return nil
}

// encodeInput encodes the input items as EncodeRequest describes.
func (f *Format) encodeInput(items []protocol.Item) ([]any, error) {
	out := make([]any, 0, len(items))
	// reasoning is the open run of reasoning items under Options.ResendReasoningText, which goes
	// out only ahead of an assistant message or a function_call, and is dropped otherwise.
	var reasoning []any
	for i, it := range items {
		var w any
		switch it := it.(type) {
		case protocol.Reasoning:
			r, ok := f.encodeReasoning(it)
			if !ok {
				continue
			}
			if f.opts.ResendReasoningText {
				reasoning = append(reasoning, r)
				continue
			}
			w = r
		case protocol.Message:
			content, err := encodeContent(it.Content)
			if err != nil {
				return nil, fmt.Errorf("input %d: %w", i, err)
			}
			if it.Role == protocol.RoleAssistant {
				out = append(out, reasoning...)
			}
			w = messageItem{Type: "message", ID: it.ID, Role: string(it.Role), Content: content}
		case protocol.FunctionCall:
			out = append(out, reasoning...)
			args := it.Arguments
			if args == "" {
				args = "{}"
			}
			w = functionCallItem{Type: "function_call", ID: it.ID, CallID: it.CallID, Name: it.Name, Arguments: args}
		case protocol.FunctionCallOutput:
			o := functionCallOutputItem{Type: "function_call_output", ID: it.ID, CallID: it.CallID, Output: it.Output}
			if len(it.OutputParts) > 0 {
				parts, err := encodeContent(it.OutputParts)
				if err != nil {
					return nil, fmt.Errorf("input %d: output %w", i, err)
				}
				o.Output = parts
			}
			w = o
		default:
			return nil, fmt.Errorf("input %d: item %T has no Open Responses form", i, it)
		}
		reasoning = nil
		out = append(out, w)
	}
	return out, nil
}

// encodeReasoning returns the reasoning item r goes out as, in the form the format's Options
// give, and whether it goes out at all, as EncodeRequest describes; a run that no assistant
// message or function_call follows is encodeInput's to drop.
func (f *Format) encodeReasoning(r protocol.Reasoning) (reasoningItem, bool) {
	e, ok := openEnvelope(r.EncryptedContent)
	if !ok {
		return reasoningItem{}, false
	}
	w := reasoningItem{Type: "reasoning", ID: r.ID, Summary: make([]textPart, 0, len(r.Summary)), EncryptedContent: e.encrypted}
	for _, s := range r.Summary {
		w.Summary = append(w.Summary, textPart{Type: "summary_text", Text: s.Text})
	}
	if !f.opts.ResendReasoningText {
		return w, e.encrypted != ""
	}
	for _, t := range e.content {
		w.Content = append(w.Content, textPart{Type: "reasoning_text", Text: t})
	}
	return w, e.encrypted != "" || len(e.content) > 0
}

// opened is an envelope's members, read.
type opened struct {
	encrypted string
	content   []string
}

// openEnvelope returns what the envelope sealed in encryptedContent holds, and whether
// encryptedContent is this format's envelope at all: false for one that is empty, another
// format's or version's, or malformed, a well-formed envelope having both of its members.
func openEnvelope(encryptedContent string) (opened, bool) {
	rest, ok := strings.CutPrefix(encryptedContent, envelopePrefix)
	if !ok {
		return opened{}, false
	}
	b, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return opened{}, false
	}
	var e envelope
	if json.Unmarshal(b, &e) != nil || e.EncryptedContent == nil || e.Content == nil {
		return opened{}, false
	}
	return opened{encrypted: *e.EncryptedContent, content: *e.Content}, true
}

// encodeToolChoice encodes a tool choice as tool_choice, nil when the request sets none.
func encodeToolChoice(tc protocol.ToolChoice) (any, error) {
	switch tc := tc.(type) {
	case nil:
		return nil, nil
	case protocol.ToolChoiceMode:
		return string(tc), nil
	case protocol.FunctionToolChoice:
		return specificFunction{Type: "function", Name: tc.Name}, nil
	case protocol.AllowedToolChoice:
		w := allowedTools{Type: "allowed_tools", Mode: string(tc.Mode)}
		for _, f := range tc.Tools {
			w.Tools = append(w.Tools, specificFunction{Type: "function", Name: f.Name})
		}
		return w, nil
	default:
		return nil, fmt.Errorf("tool choice %T has no Open Responses form", tc)
	}
}

// encodeContent encodes a message's content parts as EncodeRequest describes.
func encodeContent(parts []protocol.ContentPart) ([]any, error) {
	out := make([]any, 0, len(parts))
	for i, p := range parts {
		var w any
		switch p := p.(type) {
		case protocol.InputText:
			w = textPart{Type: "input_text", Text: p.Text}
		case protocol.InputImage:
			w = imagePart{Type: "input_image", ImageURL: p.ImageURL, Detail: string(p.Detail)}
		case protocol.InputAudio:
			w = audioPart{Type: audioType, Data: base64.StdEncoding.EncodeToString(p.Data), Format: string(p.Format)}
		case protocol.OutputText:
			w = textPart{Type: "output_text", Text: p.Text}
		case protocol.Refusal:
			w = refusalPart{Type: "refusal", Refusal: p.Refusal}
		default:
			return nil, fmt.Errorf("content %d: part %T has no Open Responses form", i, p)
		}
		out = append(out, w)
	}
	return out, nil
}

// encodeText encodes the text parameter, nil when it sets neither a format nor a verbosity.
func encodeText(t protocol.TextParam) (*textParam, error) {
	var w textParam
	switch f := t.Format.(type) {
	case nil:
	case protocol.PlainTextFormat:
		w.Format = &textFormat{Type: "text"}
	case protocol.JSONSchemaFormat:
		w.Format = &textFormat{
			Type:        "json_schema",
			Name:        f.Name,
			Description: f.Description,
			Schema:      f.Schema,
			Strict:      f.Strict,
		}
	default:
		return nil, fmt.Errorf("text format %T has no Open Responses form", f)
	}
	w.Verbosity = string(t.Verbosity)
	if w == (textParam{}) {
		return nil, nil
	}
	return &w, nil
}
