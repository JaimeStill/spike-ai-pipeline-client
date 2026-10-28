package responses

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/JaimeStill/spike-ai-pipeline-client/internal/native"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// EncodeRequest encodes req, which the client has validated, as a create-response body under the
// spec's names: model; instructions, omitted when there are none; input, always an array of
// items; text, its format and verbosity, omitted when the request sets neither; and
// max_output_tokens, omitted when it is zero. It adds no header.
//
// Every request is stateless and unstreamed: it carries stream false, store false, so the
// endpoint keeps nothing, and include ["reasoning.encrypted_content"], so the reasoning comes
// back in a form the next request can send; previous_response_id is never sent.
//
// A message goes out as a message item of its role, its id when it has one, and its content as
// an array of parts: input_text, input_image with its image_url and its detail when set,
// output_text, and refusal. Its status, ignored on input, is never sent. An input_audio part
// goes out as the client extension, {"type":"spike-ai-pipeline:input_audio","data","format"},
// its data the clip's bytes in standard base64 and its format wav or mp3.
//
// This version of the format sends messages alone: a function_call, a function_call_output, or a
// reasoning item is refused, as are tools, a tool choice, and reasoning, which a later version
// sends.
//
// Native's members, the native tier, are merged into the top level, and a member named like one
// the typed fields set is refused, as are stream, store, and include, which are always set.
// previous_response_id and background are refused too: each asks the endpoint to keep state, a
// prior response or a running one, which a stateless client never names.
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
	switch {
	case len(req.Tools) > 0:
		return nil, errors.New("tools aren't sent by this version of the format")
	case req.ToolChoice != nil:
		return nil, errors.New("a tool choice isn't sent by this version of the format")
	case req.Reasoning != protocol.ReasoningParam{}:
		return nil, errors.New("reasoning isn't sent by this version of the format")
	}
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
	return native.Marshal(request{
		Model:           req.Model,
		Instructions:    req.Instructions,
		Input:           input,
		Text:            text,
		MaxOutputTokens: req.MaxOutputTokens,
		Stream:          stream,
		Store:           false,
		Include:         include,
		Native:          req.Native,
	})
}

// refuseStateful refuses a Native, which the client has validated as a JSON object or empty, that
// has a previous_response_id or background member, as EncodeRequest describes.
func refuseStateful(v jsontext.Value) error {
	if len(v) == 0 {
		return nil
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(v, &members); err != nil {
		return fmt.Errorf("native: %w", err)
	}
	for _, name := range []string{"previous_response_id", "background"} {
		if _, ok := members[name]; ok {
			return fmt.Errorf("native member %s asks the endpoint to keep state, which a stateless request never does", name)
		}
	}
	return nil
}

// encodeInput encodes the input items as EncodeRequest describes.
func (f *Format) encodeInput(items []protocol.Item) ([]any, error) {
	out := make([]any, 0, len(items))
	for i, it := range items {
		m, ok := it.(protocol.Message)
		if !ok {
			return nil, fmt.Errorf("input %d: item %T isn't sent by this version of the format", i, it)
		}
		content, err := encodeContent(m.Content)
		if err != nil {
			return nil, fmt.Errorf("input %d: %w", i, err)
		}
		out = append(out, messageItem{Type: "message", ID: m.ID, Role: string(m.Role), Content: content})
	}
	return out, nil
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
