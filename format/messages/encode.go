package messages

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

// EncodeRequest encodes req, which the client has validated, as a Messages request body, and
// sets the anthropic-version header to the version the format speaks, 2026-10-21. MaxOutputTokens
// goes out as max_tokens, which Messages requires, so a request with no MaxOutputTokens is
// refused. Instructions go out as the top-level system string, and the input items as messages:
//
//	user message          a user message of text and image blocks
//	system, developer     a system message of text blocks, where it stands in the conversation
//	assistant run         one assistant message: thinking and redacted_thinking blocks from the
//	                      reasoning items that open the run, text blocks from its assistant
//	                      message, an output_text or a refusal each, and tool_use blocks from
//	                      the function_calls after it
//	function_call_output  a tool_result block of a user message
//
// Messages has no developer role, and llama.cpp's router passes a system message through where
// it stands, so a developer message goes out as a system message. Every message's content is an
// array of blocks, even of one text block.
//
// A user turn is a run of adjacent user messages and function_call_outputs, and goes out as one
// user message: every tool_result block first, in order, then the messages' blocks, in order, as
// Messages requires a user message's tool_result blocks to lead it. A tool_result's tool_use_id
// is the output's call_id, and its content the output string, or the output's parts as text
// blocks.
//
// An assistant run opens at a reasoning item, at an assistant message no reasoning item opened,
// or at a function_call no assistant message or reasoning item opened, and takes the
// function_calls that follow it, so its blocks go out thinking first, then text, then tool_use.
// A reasoning item always opens a new run, joined by the reasoning items right after it. A
// tool_use block's id is the call's call_id, its name the call's, and its input the call's
// arguments as JSON, with empty arguments, a call of none, sent as {}.
//
// A reasoning item goes out through its encrypted_content alone: the envelope DecodeResponse
// wrote there, which holds the reply's thinking block, its text and signature, or its
// redacted_thinking block, its data, each sent back as it came. Its content, which the spec's
// input reasoning item lacks, is never sent. A reasoning item whose encrypted_content isn't this
// format's envelope, being empty, another format's, another version's, or malformed, is dropped:
// it is never sent, and it is never an error, so a conversation can carry another format's
// reasoning through this one. So is a run of reasoning that no assistant message or
// function_call follows, as Chat Completions drops it: llama.cpp's router makes an assistant
// message of it, which the reasoning alone leaves without content.
//
// An input_image whose URL is a base64 data URL, as protocol.DataURL builds, goes out as an image
// block of a base64 source, its media type the data URL's; one of any other URL goes out as an
// image block of a url source, which the endpoint fetches. A data URL that isn't base64, or has
// no media type, is refused, as a base64 source needs both. An input_audio part has no Messages
// form, and is refused.
//
// Tools go out as custom tools: name, description, input_schema from Parameters, and strict. A
// tool of no Parameters goes out with input_schema {"type":"object"}, a function of no arguments,
// as Messages requires input_schema. The tool choice goes out as tool_choice: auto as
// {"type":"auto"}, required as {"type":"any"}, none as {"type":"none"}, and a named function as
// {"type":"tool","name"}. Messages has no allowed-tools choice, so an allowed_tools choice sends
// only the tools it allows, in the request's order, with its mode mapped as a mode is.
//
// Reasoning's effort none goes out as thinking {"type":"disabled"}, and any other as thinking
// {"type":"adaptive"} with output_config.effort of the same name. Text's JSON-schema format goes
// out as output_config.format, {"type":"json_schema","schema"}; plain text, the endpoint's
// default, sends nothing.
//
// The hints Messages has no form for are dropped, never refused: an image's detail; text's
// verbosity; and a JSON-schema format's name, description, and strict flag.
//
// The request is unstreamed, sent with stream false. Native's members, the native tier, are
// merged into the top level, and a member named like one the typed fields set is refused, as is
// stream, which is always set. The header is set only once the body is encoded, so a refused
// request adds none.
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
	var err error
	body := request{
		Model:     req.Model,
		MaxTokens: req.MaxOutputTokens,
		System:    req.Instructions,
		Native:    req.Native,
	}
	if body.Messages, err = encodeInput(req.Input); err != nil {
		return nil, err
	}
	if body.Tools, body.ToolChoice, err = encodeTools(req.Tools, req.ToolChoice); err != nil {
		return nil, err
	}
	var config outputConfig
	switch effort := req.Reasoning.Effort; effort {
	case "":
	case protocol.ReasoningEffortNone:
		body.Thinking = &thinking{Type: "disabled"}
	default:
		body.Thinking = &thinking{Type: "adaptive"}
		config.Effort = string(effort)
	}
	switch f := req.Text.Format.(type) {
	case nil, protocol.PlainTextFormat:
	case protocol.JSONSchemaFormat:
		config.Format = &outputFormat{Type: "json_schema", Schema: f.Schema}
	default:
		return nil, fmt.Errorf("text format %T has no Messages form", f)
	}
	if config != (outputConfig{}) {
		body.OutputConfig = &config
	}
	return native.Marshal(body)
}

// encodeInput encodes the input items as messages, joining each user turn into one user message
// and each assistant run into one assistant message, as EncodeRequest describes.
func encodeInput(items []protocol.Item) ([]message, error) {
	var messages []message
	// results and content are the open user turn's tool_result blocks and its messages' blocks,
	// both nil when no user turn is open.
	var results, content []any
	// run is the index in messages of the open run's assistant message, or -1 when no run is
	// open or the open run is reasoning alone so far. reasoning is that reasoning's blocks,
	// which lead the assistant message that joins the run.
	run, reasoning := -1, []any(nil)
	closeTurn := func() {
		if results == nil && content == nil {
			return
		}
		messages = append(messages, message{Role: string(protocol.RoleUser), Content: append(results, content...)})
		results, content = nil, nil
	}
	// openRun appends the assistant message of the run's reasoning, and opens the run at it.
	openRun := func() {
		closeTurn()
		messages = append(messages, message{Role: string(protocol.RoleAssistant), Content: reasoning})
		run, reasoning = len(messages)-1, nil
	}
	for i, it := range items {
		switch it := it.(type) {
		case protocol.Reasoning:
			b, ok := openEnvelope(it.EncryptedContent)
			if !ok {
				continue
			}
			closeTurn()
			run = -1
			reasoning = append(reasoning, b)
		case protocol.Message:
			blocks, err := encodeContent(it.Content)
			if err != nil {
				return nil, fmt.Errorf("input %d: %w", i, err)
			}
			switch it.Role {
			case protocol.RoleUser:
				run, reasoning = -1, nil
				content = append(content, blocks...)
			case protocol.RoleAssistant:
				openRun()
				messages[run].Content = append(messages[run].Content, blocks...)
			default:
				// System and developer messages stand alone, as system messages.
				closeTurn()
				run, reasoning = -1, nil
				messages = append(messages, message{Role: string(protocol.RoleSystem), Content: blocks})
			}
		case protocol.FunctionCall:
			if run < 0 {
				openRun()
			}
			args := it.Arguments
			if args == "" {
				args = "{}"
			}
			messages[run].Content = append(messages[run].Content, toolUseBlock{
				Type:  "tool_use",
				ID:    it.CallID,
				Name:  it.Name,
				Input: jsontext.Value(args),
			})
		case protocol.FunctionCallOutput:
			b, err := encodeOutput(it)
			if err != nil {
				return nil, fmt.Errorf("input %d: %w", i, err)
			}
			run, reasoning = -1, nil
			results = append(results, b)
		default:
			return nil, fmt.Errorf("input %d: item %T has no Messages form", i, it)
		}
	}
	closeTurn()
	return messages, nil
}

// openEnvelope returns the block the envelope sealed in encryptedContent holds, a thinkingBlock
// or a redactedThinkingBlock, and whether encryptedContent is this format's envelope at all:
// false for one that is empty, another format's or version's, or malformed, a well-formed
// envelope being of exactly one of the two shapes envelope describes.
func openEnvelope(encryptedContent string) (any, bool) {
	rest, ok := strings.CutPrefix(encryptedContent, envelopePrefix)
	if !ok {
		return nil, false
	}
	b, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return nil, false
	}
	var e envelope
	if json.Unmarshal(b, &e) != nil {
		return nil, false
	}
	switch {
	case e.Thinking != nil && e.Signature != nil && e.Data == nil:
		return thinkingBlock{Type: "thinking", Thinking: *e.Thinking, Signature: *e.Signature}, true
	case e.Data != nil && e.Thinking == nil && e.Signature == nil:
		return redactedThinkingBlock{Type: "redacted_thinking", Data: *e.Data}, true
	}
	return nil, false
}

// encodeOutput encodes a function_call_output as a tool_result block: its output string, or its
// parts as text blocks.
func encodeOutput(o protocol.FunctionCallOutput) (toolResultBlock, error) {
	b := toolResultBlock{Type: "tool_result", ToolUseID: o.CallID, Content: o.Output}
	if len(o.OutputParts) > 0 {
		parts, err := encodeContent(o.OutputParts)
		if err != nil {
			return toolResultBlock{}, fmt.Errorf("output %w", err)
		}
		b.Content = parts
	}
	return b, nil
}

// encodeTools encodes the request's tools and tool choice, as EncodeRequest describes, narrowing
// the tools to an allowed_tools choice's set.
func encodeTools(tools []protocol.FunctionTool, choice protocol.ToolChoice) ([]tool, *toolChoice, error) {
	var wire *toolChoice
	// allowed is the set an allowed_tools choice narrows the tools to, nil for any other choice.
	var allowed map[string]bool
	switch c := choice.(type) {
	case nil:
	case protocol.ToolChoiceMode:
		wire = &toolChoice{Type: toolChoiceType(c)}
	case protocol.FunctionToolChoice:
		wire = &toolChoice{Type: "tool", Name: c.Name}
	case protocol.AllowedToolChoice:
		wire = &toolChoice{Type: toolChoiceType(c.Mode)}
		allowed = make(map[string]bool, len(c.Tools))
		for _, f := range c.Tools {
			allowed[f.Name] = true
		}
	default:
		return nil, nil, fmt.Errorf("tool choice %T has no Messages form", c)
	}
	var out []tool
	for _, t := range tools {
		if allowed != nil && !allowed[t.Name] {
			continue
		}
		schema := t.Parameters
		if len(schema) == 0 {
			schema = jsontext.Value(`{"type":"object"}`)
		}
		out = append(out, tool{Name: t.Name, Description: t.Description, InputSchema: schema, Strict: t.Strict})
	}
	return out, wire, nil
}

// toolChoiceType returns the tool_choice type of a mode: auto as auto, required as any, and none
// as none.
func toolChoiceType(m protocol.ToolChoiceMode) string {
	if m == protocol.ToolChoiceRequired {
		return "any"
	}
	return string(m)
}

// encodeContent encodes a message's content parts as blocks, as EncodeRequest describes.
func encodeContent(parts []protocol.ContentPart) ([]any, error) {
	blocks := make([]any, 0, len(parts))
	for i, p := range parts {
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
				return nil, fmt.Errorf("content %d: %w", i, err)
			}
			b = imageBlock{Type: "image", Source: source}
		default:
			return nil, fmt.Errorf("content %d: part %T has no Messages form", i, p)
		}
		blocks = append(blocks, b)
	}
	return blocks, nil
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
