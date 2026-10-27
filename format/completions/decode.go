package completions

import (
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/JaimeStill/spike-ai-pipeline-client/format"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// DecodeResponse decodes a Chat Completions response body. The first choice's message's
// reasoning_content, when it has one, becomes a reasoning item ahead of the others: its content
// one reasoning_text part of the text, and its encrypted_content the format's envelope holding
// it, which EncodeRequest reads back. The message then becomes an assistant message item, its content an output_text part and its refusal a refusal part;
// a message with neither gives no message item. Each of its tool_calls then becomes a
// function_call item, in order, its call_id the call's id; a call of a type other than function
// is an error. The first choice's finish_reason gives the response's status, which the items
// take:
//
//	stop, tool_calls  completed
//	length            incomplete, reason max_output_tokens
//	content_filter    incomplete, reason content_filter
//
// Any other finish_reason, or a body with no choices, is an error. The reply's usage gives the
// response's: prompt_tokens, completion_tokens, total_tokens, and
// completion_tokens_details.reasoning_tokens.
func (*Format) DecodeResponse(body []byte) (*protocol.Response, error) {
	var wire response
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("completions: decode response: %w", err)
	}
	if len(wire.Choices) == 0 {
		return nil, errors.New("completions: response has no choices")
	}
	choice := wire.Choices[0]
	resp := &protocol.Response{Usage: decodeUsage(wire.Usage)}
	var err error
	if resp.Status, resp.IncompleteDetails, err = decodeFinish(choice.FinishReason); err != nil {
		return nil, fmt.Errorf("completions: response %w", err)
	}
	if r := choice.Message.ReasoningContent; r != "" {
		sealed, err := sealEnvelope(envelope{ReasoningContent: r})
		if err != nil {
			return nil, fmt.Errorf("completions: reasoning: %w", err)
		}
		resp.Output = append(resp.Output, protocol.Reasoning{
			Content:          []protocol.ReasoningText{{Text: r}},
			EncryptedContent: sealed,
		})
	}
	var content []protocol.ContentPart
	if choice.Message.Content != "" {
		content = append(content, protocol.OutputText{Text: choice.Message.Content})
	}
	if choice.Message.Refusal != "" {
		content = append(content, protocol.Refusal{Refusal: choice.Message.Refusal})
	}
	if len(content) > 0 {
		resp.Output = append(resp.Output, protocol.Message{
			Status:  resp.Status,
			Role:    protocol.RoleAssistant,
			Content: content,
		})
	}
	for i, c := range choice.Message.ToolCalls {
		if c.Type != "function" {
			return nil, fmt.Errorf("completions: response tool call %d has unknown type %q", i, c.Type)
		}
		resp.Output = append(resp.Output, protocol.FunctionCall{
			Status:    resp.Status,
			CallID:    c.ID,
			Name:      c.Function.Name,
			Arguments: c.Function.Arguments,
		})
	}
	return resp, nil
}

// decodeUsage returns a reply's usage as the protocol's.
func decodeUsage(u usage) protocol.Usage {
	return protocol.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		TotalTokens:  u.TotalTokens,
		OutputTokensDetails: protocol.OutputTokensDetails{
			ReasoningTokens: u.CompletionTokensDetails.ReasoningTokens,
		},
	}
}

// decodeFinish returns the status, and the incomplete details, that a finish_reason gives, as
// DecodeResponse describes.
func decodeFinish(reason string) (protocol.Status, *protocol.IncompleteDetails, error) {
	switch reason {
	case "stop", "tool_calls":
		return protocol.StatusCompleted, nil, nil
	case "length":
		return protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}, nil
	case "content_filter":
		return protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonContentFilter}, nil
	}
	return "", nil, fmt.Errorf("has unknown finish_reason %q", reason)
}

// sealEnvelope returns e in its encrypted_content layout: envelopePrefix, then e as JSON in
// base64.
func sealEnvelope(e envelope) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return envelopePrefix + base64.StdEncoding.EncodeToString(b), nil
}

// DecodeError decodes an OpenAI-style error body with format.DecodeOpenAIError, which
// describes the shapes it reads.
func (*Format) DecodeError(body []byte) (message, typ, code string) {
	return format.DecodeOpenAIError(body)
}

// NewStreamDecoder returns a decoder for one streamed Chat Completions reply: a stream of
// chat.completion.chunk events, which it turns into the protocol's streaming events in the
// spec's order. Chat Completions names no event types, so it reads every event's data alike,
// whatever its type. Of each chunk it reads the first choice, index 0, and the usage.
//
// The first event opens the stream with response.created, its response in_progress. The
// choice's content and refusal deltas make the assistant message: output_item.added, in
// progress, when the first of them arrives; then for each run of content, or of refusal, a
// part, output_text or refusal, with content_part.added, its deltas, its done event, and
// content_part.done; and output_item.done carrying the whole message once the finish_reason
// arrives, the message taking the status it gives, as DecodeResponse maps it. A delta that is
// empty makes no event. It translates the message alone: a chunk's other members are ignored.
//
// The stream ends at data [DONE], or at the end of the stream once the finish_reason has
// arrived, where it closes with the terminal event: response.completed or
// response.incomplete, as the finish_reason gives, its response holding the output and the
// usage of the chunk that carried it. A stream that ends with no finish_reason is
// io.ErrUnexpectedEOF; so is [DONE] without one. A chunk that isn't JSON, an unknown
// finish_reason, a delta after the finish_reason, or an event after [DONE] is an error. The
// message has no id, as Chat Completions gives none, so its events' item_id is empty.
func (*Format) NewStreamDecoder() format.StreamDecoder {
	return &streamDecoder{}
}

// streamDecoder is a Chat Completions stream's decoder, as NewStreamDecoder describes.
type streamDecoder struct {
	// seq is the next event's sequence number.
	seq int
	// created is set once response.created is made, and ended once the terminal event is, or
	// an error has broken the stream.
	created, ended bool
	// finished is set once the finish_reason has arrived, giving status and details.
	finished bool
	status   protocol.Status
	details  *protocol.IncompleteDetails
	usage    protocol.Usage
	// output is the items done, in output order.
	output []protocol.Item
	// message is the message open, nil when none is.
	message *streamMessage
	// events are the events the current call has made.
	events []protocol.Event
}

// streamMessage is the assistant message a stream is making.
type streamMessage struct {
	// index is the message's place in the output.
	index int
	// content is the parts done.
	content []protocol.ContentPart
	// part is the part open: partText or partRefusal, or partNone when none is. text is its
	// text so far.
	part partKind
	text strings.Builder
}

// partKind is the kind of a message's open part.
type partKind int

// The kinds of part a stream opens in a message.
const (
	partNone partKind = iota
	partText
	partRefusal
)

// Decode decodes one event of the stream, as NewStreamDecoder describes. With an error, the
// events it returns are those it made before it.
func (d *streamDecoder) Decode(_ string, data []byte) ([]protocol.Event, error) {
	d.events = nil
	if err := d.decode(data); err != nil {
		d.ended = true
		return d.events, fmt.Errorf("completions: stream: %w", err)
	}
	return d.events, nil
}

// decode decodes data, an event's, making events in d.events.
func (d *streamDecoder) decode(data []byte) error {
	if d.ended {
		return errors.New("event after the end of the stream")
	}
	if !d.created {
		d.created = true
		d.emit(protocol.ResponseCreated{SequenceNumber: d.next(), Response: &protocol.Response{Status: protocol.StatusInProgress}})
	}
	if string(data) == "[DONE]" {
		return d.end()
	}
	var c chunk
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("decode chunk: %w", err)
	}
	if c.Usage != nil {
		d.usage = decodeUsage(*c.Usage)
	}
	for _, choice := range c.Choices {
		if choice.Index != 0 {
			continue
		}
		delta := choice.Delta
		if d.finished && delta.Content+delta.Refusal != "" {
			return errors.New("delta after the finish_reason")
		}
		d.delta(partText, delta.Content)
		d.delta(partRefusal, delta.Refusal)
		if choice.FinishReason != "" {
			if d.finished {
				return errors.New("a second finish_reason")
			}
			var err error
			if d.status, d.details, err = decodeFinish(choice.FinishReason); err != nil {
				return fmt.Errorf("chunk %w", err)
			}
			d.finished = true
			d.closeMessage(d.status)
		}
	}
	return nil
}

// End ends the stream, as NewStreamDecoder describes: with the terminal event once the
// finish_reason has arrived, or io.ErrUnexpectedEOF before it. After the terminal event or an
// error, it makes nothing.
func (d *streamDecoder) End() ([]protocol.Event, error) {
	if d.ended {
		return nil, nil
	}
	d.events = nil
	if err := d.end(); err != nil {
		return d.events, fmt.Errorf("completions: stream: %w", err)
	}
	return d.events, nil
}

// end closes the stream with its terminal event, or reports it incomplete.
func (d *streamDecoder) end() error {
	d.ended = true
	if !d.finished {
		return fmt.Errorf("stream ended with no finish_reason: %w", io.ErrUnexpectedEOF)
	}
	resp := &protocol.Response{
		Output:            slices.Clone(d.output),
		Status:            d.status,
		IncompleteDetails: d.details,
		Usage:             d.usage,
	}
	if d.status == protocol.StatusCompleted {
		d.emit(protocol.ResponseCompleted{SequenceNumber: d.next(), Response: resp})
	} else {
		d.emit(protocol.ResponseIncomplete{SequenceNumber: d.next(), Response: resp})
	}
	return nil
}

// delta appends text to the message's open part of kind, opening the message, and the part,
// when it isn't open. An empty text makes nothing.
func (d *streamDecoder) delta(kind partKind, text string) {
	if text == "" {
		return
	}
	m := d.message
	if m == nil {
		m = &streamMessage{index: len(d.output)}
		d.message = m
		d.emit(protocol.OutputItemAdded{
			SequenceNumber: d.next(),
			OutputIndex:    m.index,
			Item:           protocol.Message{Status: protocol.StatusInProgress, Role: protocol.RoleAssistant},
		})
	}
	if m.part != kind {
		d.closePart()
		m.part = kind
		var part protocol.ContentPart = protocol.OutputText{}
		if kind == partRefusal {
			part = protocol.Refusal{}
		}
		d.emit(protocol.ContentPartAdded{SequenceNumber: d.next(), OutputIndex: m.index, ContentIndex: len(m.content), Part: part})
	}
	m.text.WriteString(text)
	if kind == partRefusal {
		d.emit(protocol.RefusalDelta{SequenceNumber: d.next(), OutputIndex: m.index, ContentIndex: len(m.content), Delta: text})
	} else {
		d.emit(protocol.OutputTextDelta{SequenceNumber: d.next(), OutputIndex: m.index, ContentIndex: len(m.content), Delta: text})
	}
}

// closePart closes the message's open part, if it has one: its text's done event, then
// content_part.done.
func (d *streamDecoder) closePart() {
	m := d.message
	if m.part == partNone {
		return
	}
	text, index := m.text.String(), len(m.content)
	var part protocol.ContentPart
	if m.part == partRefusal {
		part = protocol.Refusal{Refusal: text}
		d.emit(protocol.RefusalDone{SequenceNumber: d.next(), OutputIndex: m.index, ContentIndex: index, Refusal: text})
	} else {
		part = protocol.OutputText{Text: text}
		d.emit(protocol.OutputTextDone{SequenceNumber: d.next(), OutputIndex: m.index, ContentIndex: index, Text: text})
	}
	d.emit(protocol.ContentPartDone{SequenceNumber: d.next(), OutputIndex: m.index, ContentIndex: index, Part: part})
	m.content = append(m.content, part)
	m.part = partNone
	m.text.Reset()
}

// closeMessage closes the open message, if there is one, with its open part: output_item.done
// carrying the whole message, of status.
func (d *streamDecoder) closeMessage(status protocol.Status) {
	if d.message == nil {
		return
	}
	d.closePart()
	m := protocol.Message{Status: status, Role: protocol.RoleAssistant, Content: d.message.content}
	d.emit(protocol.OutputItemDone{SequenceNumber: d.next(), OutputIndex: d.message.index, Item: m})
	d.output = append(d.output, m)
	d.message = nil
}

// emit makes e, the stream's next event.
func (d *streamDecoder) emit(e protocol.Event) {
	d.events = append(d.events, e)
}

// next returns the next event's sequence number.
func (d *streamDecoder) next() int {
	n := d.seq
	d.seq++
	return n
}
