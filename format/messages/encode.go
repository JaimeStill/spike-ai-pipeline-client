package messages

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// EncodeRequest encodes req, which the client has validated, as a Messages request body, and
// sets the anthropic-version header to the version the format speaks, 2026-10-21. MaxOutputTokens
// goes out as max_tokens, which Messages requires, so a request with no MaxOutputTokens is
// refused. Instructions go out as the top-level system string, and the input items as messages,
// each one of the same role in its place:
//
//	user               a user message of text and image blocks
//	system, developer  a system message of text blocks, where it stands in the conversation
//	assistant          an assistant message of text blocks, an output_text or a refusal each
//
// Messages has no developer role, and llama.cpp's router passes a system message through where
// it stands, so a developer message goes out as a system message. Every message's content is an
// array of blocks, even of one text block.
//
// An input_image whose URL is a base64 data URL, as protocol.DataURL builds, goes out as an image
// block of a base64 source, its media type the data URL's; one of any other URL goes out as an
// image block of a url source, which the endpoint fetches. A data URL that isn't base64, or has
// no media type, is refused, as a base64 source needs both. An image's detail has no Messages
// form, and is dropped. An input_audio part has none either, and is refused.
//
// The format doesn't yet encode the rest of the model, and refuses it rather than drop it: a
// function_call, function_call_output, or reasoning item; tools and a tool choice; a reasoning
// effort; a text format or verbosity; and a Native member. The header is set only once the body
// is encoded, so a refused request adds none.
func (*Format) EncodeRequest(req *protocol.Request, header http.Header) ([]byte, error) {
	b, err := encodeRequest(req)
	if err != nil {
		return nil, fmt.Errorf("messages: %w", err)
	}
	header.Set("anthropic-version", version)
	return b, nil
}

// EncodeStreamRequest refuses every request: the format doesn't stream yet. It adds no headers.
func (*Format) EncodeStreamRequest(*protocol.Request, http.Header) ([]byte, error) {
	return nil, errNoStreaming
}

// encodeRequest encodes req as EncodeRequest describes.
func encodeRequest(req *protocol.Request) ([]byte, error) {
	if req.MaxOutputTokens == 0 {
		return nil, errors.New("request has no max output tokens, which Messages requires as max_tokens")
	}
	if err := refuseUnsupported(req); err != nil {
		return nil, err
	}
	body := request{
		Model:     req.Model,
		MaxTokens: req.MaxOutputTokens,
		System:    req.Instructions,
	}
	for i, it := range req.Input {
		m, ok := it.(protocol.Message)
		if !ok {
			return nil, fmt.Errorf("input %d: item %T is not yet supported", i, it)
		}
		wire, err := encodeMessage(m)
		if err != nil {
			return nil, fmt.Errorf("input %d: %w", i, err)
		}
		body.Messages = append(body.Messages, wire)
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	return b, nil
}

// refuseUnsupported refuses a request that sets a field the format doesn't yet encode, as
// EncodeRequest lists them.
func refuseUnsupported(req *protocol.Request) error {
	switch {
	case len(req.Tools) > 0:
		return errors.New("tools are not yet supported")
	case req.ToolChoice != nil:
		return errors.New("a tool choice is not yet supported")
	case req.Reasoning.Effort != "":
		return errors.New("a reasoning effort is not yet supported")
	case req.Text.Format != nil:
		return errors.New("a text format is not yet supported")
	case req.Text.Verbosity != "":
		return errors.New("a verbosity is not yet supported")
	case len(req.Native) > 0:
		return errors.New("native members are not yet supported")
	}
	return nil
}

// encodeMessage encodes m as a Messages message of its role, as EncodeRequest describes.
func encodeMessage(m protocol.Message) (message, error) {
	role := string(m.Role)
	if m.Role == protocol.RoleDeveloper {
		role = string(protocol.RoleSystem)
	}
	wire := message{Role: role, Content: make([]any, 0, len(m.Content))}
	for i, p := range m.Content {
		var b any
		switch p := p.(type) {
		case protocol.InputText:
			b = textBlock{Type: "text", Text: p.Text}
		case protocol.OutputText:
			b = textBlock{Type: "text", Text: p.Text}
		case protocol.Refusal:
			// Messages has no refusal block to send back; the refusal's text stands for it.
			b = textBlock{Type: "text", Text: p.Refusal}
		case protocol.InputImage:
			source, err := encodeImageSource(p.ImageURL)
			if err != nil {
				return message{}, fmt.Errorf("content %d: %w", i, err)
			}
			b = imageBlock{Type: "image", Source: source}
		default:
			return message{}, fmt.Errorf("content %d: part %T has no Messages form", i, p)
		}
		wire.Content = append(wire.Content, b)
	}
	return wire, nil
}

// encodeImageSource returns the image source of an input_image's URL: a base64 source for a
// base64 data URL, data:<media type>;base64,<data>, and a url source for any other URL. A data
// URL that isn't base64, or has no media type, is an error.
func encodeImageSource(url string) (imageSource, error) {
	rest, ok := strings.CutPrefix(url, "data:")
	if !ok {
		return imageSource{Type: "url", URL: url}, nil
	}
	meta, data, ok := strings.Cut(rest, ",")
	if !ok {
		return imageSource{}, errors.New("image data URL has no data")
	}
	mediaType, ok := strings.CutSuffix(meta, ";base64")
	if !ok {
		return imageSource{}, errors.New("image data URL isn't base64, as a base64 source requires")
	}
	// A media type's parameters, such as a charset, precede ;base64; the source takes the type
	// alone.
	mediaType, _, _ = strings.Cut(mediaType, ";")
	if mediaType == "" {
		return imageSource{}, errors.New("image data URL has no media type, which a base64 source requires")
	}
	return imageSource{Type: "base64", MediaType: mediaType, Data: data}, nil
}
