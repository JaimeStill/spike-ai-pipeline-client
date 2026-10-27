package messages

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/JaimeStill/spike-ai-pipeline-client/format"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// NewStreamDecoder returns a decoder for one streamed Messages reply: a stream of events each
// naming its type in its data, which it turns into the protocol's streaming events in the spec's
// order.
//
// message_start opens the stream with response.created, its response in_progress, and gives the
// reply's id and its input usage. Each content block then becomes what DecodeResponse makes of
// the same block, in the same output order:
//
//	thinking           a reasoning item: output_item.added, in progress and with no content,
//	                   and its one reasoning_text part, content_part.added, at the block's
//	                   start; a reasoning.delta for each thinking_delta; and at the block's stop
//	                   reasoning.done and content_part.done. A signature_delta makes no event:
//	                   the signature is kept for the item's output_item.done, whose
//	                   encrypted_content is the envelope DecodeResponse gives the same block,
//	                   {"thinking","signature"}, which EncodeRequest reads back
//	redacted_thinking  a reasoning item of no content: output_item.added at the block's start,
//	                   and an output_item.done whose envelope holds the block's {"data"}
//	text               an output_text part of the assistant message open, or of a new one,
//	                   added at the block's start: content_part.added, an output_text.delta for
//	                   each text_delta, and at the block's stop output_text.done and
//	                   content_part.done. Consecutive text blocks are parts of one message, as
//	                   DecodeResponse joins them, and the message ends at the next block of
//	                   another type, or at message_delta
//	tool_use           a function_call item: output_item.added, in progress, with the block's
//	                   id as its call_id, its name, and no arguments, at the block's start; a
//	                   function_call_arguments.delta for each input_json_delta; and at the
//	                   block's stop function_call_arguments.done, its arguments the deltas
//	                   joined
//
// A delta that is empty makes no event. A tool_use block of no input_json_delta stops with one
// argument delta of the input its start gave, or {} when it gave none, so the call's arguments are
// the empty object DecodeResponse gives a call of no input, never empty.
//
// Items and parts may be open at once: llama.cpp's router stops every block only at the end of
// the reply, after the last block began, where Anthropic stops each before the next begins. Each
// part closes at its own block's stop, in whatever order the stops arrive. An item's
// output_item.done, carrying the whole item, follows once its blocks have stopped, a message's
// once it has ended too, and once its status is known: an item that a later item follows is
// completed, and the last item waits for message_delta's stop_reason, which gives the response's
// status as DecodeResponse maps it, so only the last item can be incomplete, as the spec has it.
// A second part of a message beginning while its first is open, which neither endpoint sends, is
// an error, as the spec adds a part only once the one ahead of it is done.
//
// message_delta's stop_reason and usage join the terminal Response: its input tokens are
// message_start's input_tokens with its cache_read_input_tokens and cache_creation_input_tokens
// added, as DecodeResponse counts them, its output tokens message_delta's output_tokens, and its
// total their sum. message_delta closes the items still to close, and message_stop then closes the
// stream with the terminal event: response.completed or response.incomplete, as the stop_reason
// gives, its response holding the output and the usage. The stream ends there; Messages sends no
// [DONE]. A stop_reason of pause_turn, which has no place in a stateless response, or one
// DecodeResponse doesn't map, is an error, as it is to DecodeResponse.
//
// ping events make nothing, as the comments the router sends to keep the connection alive never
// reach the decoder, and an event of a type the decoder doesn't know, or a delta of a type it
// doesn't know, is ignored, as Anthropic's versioning policy allows it to add them. A content
// block of a type DecodeResponse doesn't know is an error, as it is to DecodeResponse.
//
// An error frame, recognized from its data alone, is the endpoint's report that the response
// failed, and closes the stream as the spec has it: the error event, then the terminal
// response.failed, its response failed, its error the frame's code and message, holding the
// output made before the frame and the usage given so far. It takes either shape: Anthropic's,
// {"type":"error","error":{"type","message"}}, its payload the error's type and message; or a
// body with a string message whose type isn't a Messages event's, as llama.cpp's
// {"code","message","type":"server_error"} is, its payload the body's type, code, and message.
// The items open at the frame get no done event: they are in the output in progress, in output
// order, with the content or arguments they had, as the Accumulator holds an item still open. A
// frame as the first event follows response.created, which every stream opens with, and an event
// after the frame is an error.
//
// An event that isn't JSON, any event but ping before message_start, a second message_start, a
// block whose index began before, a delta or a stop of a block that didn't begin or has stopped,
// a delta of a known type for a block of another type, message_delta while a block is open, a
// block or a second message_delta after message_delta, message_stop before message_delta, and an
// event after message_stop are errors, after which the stream is broken. A stream that ends before
// message_stop is io.ErrUnexpectedEOF. Each item's id, its events' item_id, is the one
// DecodeResponse gives it: itemID derives it from the reply's id and the item's output index.
func (*Format) NewStreamDecoder() format.StreamDecoder {
	return &streamDecoder{}
}

// streamDecoder is a Messages stream's decoder, as NewStreamDecoder describes.
type streamDecoder struct {
	// seq is the next event's sequence number.
	seq int
	// created is set once response.created is made, started once message_start is read, and
	// ended once the terminal event is made or an error has broken the stream.
	created, started, ended bool
	// id is the reply's id, message_start's.
	id string
	// usage is message_start's usage, its output_tokens message_delta's once that arrives.
	usage usage
	// stopped is set once message_delta has arrived, giving status and details.
	stopped bool
	status  protocol.Status
	details *protocol.IncompleteDetails
	// blocks are the content blocks begun, by index.
	blocks map[int]*streamBlock
	// items are the output items added, in output order, done and open.
	items []*streamItem
	// events are the events the current call has made.
	events []protocol.Event
}

// streamItem is an output item a stream is making.
type streamItem struct {
	// id is the item's id, and index its place in the output.
	id    string
	index int
	// blocks are the content blocks the item is made of: a message's, one per part, in order,
	// and a reasoning item's or a function call's one.
	blocks []*streamBlock
	// ended is set once a message takes no more parts; a reasoning item or a function call is
	// one block, and ends at once.
	ended bool
	// done is set once output_item.done is made, carrying item.
	done bool
	item protocol.Item
}

// streamBlock is a content block a stream is making.
type streamBlock struct {
	// typ is the block's type: thinking, redacted_thinking, text, or tool_use.
	typ string
	// item is the output item the block belongs to, and part its content index in a message.
	item *streamItem
	part int
	// stopped is set at the block's content_block_stop.
	stopped bool
	// text is a thinking block's thinking, a text block's text, or a tool_use block's
	// arguments, so far; deltas counts the tool_use block's input_json_deltas.
	text   strings.Builder
	deltas int
	// signature is a thinking block's signature, and data a redacted_thinking block's.
	signature, data string
	// callID, name, and input are a tool_use block's id, name, and the input its start gave.
	callID, name string
	input        jsontext.Value
}

// eventTypes are the Messages stream's event types. A body of another type with a string message
// is an error frame, as llama.cpp's is.
var eventTypes = map[string]bool{
	"message_start":       true,
	"message_delta":       true,
	"message_stop":        true,
	"content_block_start": true,
	"content_block_delta": true,
	"content_block_stop":  true,
	"ping":                true,
	"error":               true,
}

// Decode decodes one event of the stream, as NewStreamDecoder describes. With an error, the
// events it returns are those it made before it.
func (d *streamDecoder) Decode(data []byte) ([]protocol.Event, error) {
	d.events = nil
	if err := d.decode(data); err != nil {
		d.ended = true
		return d.events, fmt.Errorf("messages: stream: %w", err)
	}
	return d.events, nil
}

// decode decodes data, an event's, making events in d.events.
func (d *streamDecoder) decode(data []byte) error {
	if d.ended {
		return errors.New("event after the end of the stream")
	}
	var e streamEvent
	if err := json.Unmarshal(data, &e); err != nil {
		// The JSON error is formatted, not wrapped: an event cut short wraps
		// io.ErrUnexpectedEOF, which in a stream means the stream ended before its terminal
		// event, and this event arrived whole.
		return fmt.Errorf("decode event: %v", err)
	}
	if e.Type == "error" || (!eventTypes[e.Type] && e.Message.Kind() == jsontext.KindString) {
		d.fail(e.Type == "error", data)
		return nil
	}
	if e.Type == "ping" || !eventTypes[e.Type] {
		return nil
	}
	if e.Type == "message_start" {
		return d.start(e.Message)
	}
	if !d.started {
		return fmt.Errorf("%s before message_start", e.Type)
	}
	switch e.Type {
	case "content_block_start":
		return d.startBlock(e.Index, e.ContentBlock)
	case "content_block_delta":
		return d.delta(e.Index, e.Delta)
	case "content_block_stop":
		return d.stopBlock(e.Index)
	case "message_delta":
		return d.messageDelta(e.Delta.StopReason, e.Usage.OutputTokens)
	default: // message_stop
		return d.end()
	}
}

// start reads message_start's message, opening the stream.
func (d *streamDecoder) start(message jsontext.Value) error {
	if d.started {
		return errors.New("a second message_start")
	}
	var m streamMessage
	if len(message) > 0 {
		if err := json.Unmarshal(message, &m); err != nil {
			return fmt.Errorf("decode message_start's message: %v", err)
		}
	}
	d.started, d.id, d.usage = true, m.ID, m.Usage
	d.usage.OutputTokens = 0
	d.blocks = map[int]*streamBlock{}
	d.create()
	return nil
}

// create makes response.created, once.
func (d *streamDecoder) create() {
	if d.created {
		return
	}
	d.created = true
	d.emit(protocol.ResponseCreated{SequenceNumber: d.next(), Response: &protocol.Response{Status: protocol.StatusInProgress}})
}

// startBlock begins the content block at index, b, as NewStreamDecoder describes.
func (d *streamDecoder) startBlock(index int, b block) error {
	switch {
	case d.stopped:
		return fmt.Errorf("content block %d began after message_delta", index)
	case d.blocks[index] != nil:
		return fmt.Errorf("content block %d began twice", index)
	}
	sb := &streamBlock{typ: b.Type}
	if b.Type == "text" {
		if m := d.openMessage(); m != nil {
			if last := m.blocks[len(m.blocks)-1]; !last.stopped {
				return fmt.Errorf("text block %d began while the message's text block before it is open", index)
			}
			sb.item, sb.part = m, len(m.blocks)
			m.blocks = append(m.blocks, sb)
		} else if err := d.addItem(sb); err != nil {
			return err
		}
		d.blocks[index] = sb
		d.emit(protocol.ContentPartAdded{SequenceNumber: d.next(), ItemID: sb.item.id, OutputIndex: sb.item.index, ContentIndex: sb.part, Part: protocol.OutputText{}})
		d.writeText(sb, b.Text)
		return nil
	}
	switch b.Type {
	case "thinking":
		if err := d.addItem(sb); err != nil {
			return err
		}
		d.emit(protocol.ContentPartAdded{SequenceNumber: d.next(), ItemID: sb.item.id, OutputIndex: sb.item.index, Part: protocol.ReasoningText{}})
		d.writeText(sb, b.Thinking)
		sb.signature = b.Signature
	case "redacted_thinking":
		sb.data = b.Data
		if err := d.addItem(sb); err != nil {
			return err
		}
	case "tool_use":
		sb.callID, sb.name, sb.input = b.ID, b.Name, b.Input
		if err := d.addItem(sb); err != nil {
			return err
		}
	default:
		return fmt.Errorf("content block %d has unknown type %q", index, b.Type)
	}
	d.blocks[index] = sb
	return nil
}

// openMessage returns the assistant message still taking parts, the last item, or nil when
// there is none.
func (d *streamDecoder) openMessage() *streamItem {
	if n := len(d.items); n > 0 {
		if last := d.items[n-1]; !last.ended {
			return last
		}
	}
	return nil
}

// addItem adds the item of b, its first block, at the end of the output: the message the new
// item follows takes no more parts, the items before the new one close completed if their blocks
// have stopped, and then the new one's output_item.added, in progress and of no content. A
// reasoning item or a function call ends at once; a message takes parts until a block of another
// type begins, or message_delta arrives.
func (d *streamDecoder) addItem(b *streamBlock) error {
	if m := d.openMessage(); m != nil {
		m.ended = true
	}
	it := &streamItem{id: itemID(d.id, len(d.items)), index: len(d.items), blocks: []*streamBlock{b}, ended: b.typ != "text"}
	b.item = it
	d.items = append(d.items, it)
	// The new item follows every item before it, so those whose blocks have stopped are
	// completed, and close ahead of its output_item.added.
	if err := d.settle(it.index); err != nil {
		return err
	}
	var added protocol.Item
	switch b.typ {
	case "text":
		added = protocol.Message{ID: it.id, Status: protocol.StatusInProgress, Role: protocol.RoleAssistant}
	case "tool_use":
		added = protocol.FunctionCall{ID: it.id, Status: protocol.StatusInProgress, CallID: b.callID, Name: b.name}
	default:
		added = protocol.Reasoning{ID: it.id, Status: protocol.StatusInProgress}
	}
	d.emit(protocol.OutputItemAdded{SequenceNumber: d.next(), OutputIndex: it.index, Item: added})
	return nil
}

// writeText appends text, a thinking or text block's, to b, with its delta event. An empty text
// makes nothing.
func (d *streamDecoder) writeText(b *streamBlock, text string) {
	if text == "" {
		return
	}
	b.text.WriteString(text)
	it := b.item
	if b.typ == "thinking" {
		d.emit(protocol.ReasoningDelta{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, Delta: text})
	} else {
		d.emit(protocol.OutputTextDelta{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: b.part, Delta: text})
	}
}

// delta reads a content_block_delta of the block at index, as NewStreamDecoder describes.
func (d *streamDecoder) delta(index int, delta streamDelta) error {
	b, err := d.openBlock(index)
	if err != nil {
		return err
	}
	// want is the type of block a delta of a known type belongs to.
	var want string
	switch delta.Type {
	case "text_delta":
		want = "text"
	case "thinking_delta", "signature_delta":
		want = "thinking"
	case "input_json_delta":
		want = "tool_use"
	default:
		return nil
	}
	if b.typ != want {
		return fmt.Errorf("%s of content block %d, a %s block", delta.Type, index, b.typ)
	}
	switch delta.Type {
	case "text_delta":
		d.writeText(b, delta.Text)
	case "thinking_delta":
		d.writeText(b, delta.Thinking)
	case "signature_delta":
		b.signature += delta.Signature
	case "input_json_delta":
		d.writeArgs(b, delta.PartialJSON)
	}
	return nil
}

// writeArgs appends args to a tool_use block's arguments, with its delta event. Empty args make
// nothing.
func (d *streamDecoder) writeArgs(b *streamBlock, args string) {
	b.deltas++
	if args == "" {
		return
	}
	b.text.WriteString(args)
	d.emit(protocol.FunctionCallArgumentsDelta{SequenceNumber: d.next(), ItemID: b.item.id, OutputIndex: b.item.index, Delta: args})
}

// openBlock returns the block at index, begun and not stopped.
func (d *streamDecoder) openBlock(index int) (*streamBlock, error) {
	b := d.blocks[index]
	switch {
	case b == nil:
		return nil, fmt.Errorf("content block %d didn't begin", index)
	case b.stopped:
		return nil, fmt.Errorf("content block %d has stopped", index)
	}
	return b, nil
}

// stopBlock reads the content_block_stop of the block at index: its part's done events, or its
// arguments', and then its item's output_item.done if it may close.
func (d *streamDecoder) stopBlock(index int) error {
	b, err := d.openBlock(index)
	if err != nil {
		return err
	}
	b.stopped = true
	it := b.item
	text := b.text.String()
	switch b.typ {
	case "text":
		d.emit(protocol.OutputTextDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: b.part, Text: text})
		d.emit(protocol.ContentPartDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: b.part, Part: protocol.OutputText{Text: text}})
	case "thinking":
		d.emit(protocol.ReasoningDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, Text: text})
		d.emit(protocol.ContentPartDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, Part: protocol.ReasoningText{Text: text}})
	case "tool_use":
		if b.deltas == 0 {
			// A call of no input_json_delta has the input its start gave, {} from
			// Anthropic, or none from an endpoint that omits it; DecodeResponse gives
			// the same call's input, {}, as its arguments.
			input := string(b.input)
			if input == "" {
				input = "{}"
			}
			d.writeArgs(b, input)
		}
		d.emit(protocol.FunctionCallArgumentsDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, Arguments: b.text.String()})
	}
	return d.settle(len(d.items))
}

// messageDelta reads message_delta's stop_reason and output tokens, and closes the items still
// to close: the last of the status the stop_reason gives, those before it completed.
func (d *streamDecoder) messageDelta(stopReason string, outputTokens int) error {
	if d.stopped {
		return errors.New("a second message_delta")
	}
	for i, it := range d.items {
		if !it.stoppedAll() {
			return fmt.Errorf("message_delta while a content block of output %d is open", i)
		}
	}
	var ok bool
	if d.status, d.details, ok = decodeStop(stopReason); !ok {
		return fmt.Errorf("message_delta has unknown stop_reason %q", stopReason)
	}
	d.stopped = true
	d.usage.OutputTokens = outputTokens
	if m := d.openMessage(); m != nil {
		m.ended = true
	}
	return d.settle(len(d.items))
}

// settle closes, in output order, each of the first n items that may close: each not done
// whose blocks have stopped and that has ended, and whose status is known, completed when an
// item follows it among the n, and the stop_reason's once message_delta has given it.
func (d *streamDecoder) settle(n int) error {
	for _, it := range d.items[:n] {
		if it.done || !it.ended || !it.stoppedAll() {
			continue
		}
		status := protocol.StatusCompleted
		if it.index == len(d.items)-1 {
			if !d.stopped {
				continue
			}
			status = d.status
		}
		if err := d.closeItem(it, status); err != nil {
			return err
		}
	}
	return nil
}

// stoppedAll reports whether every block of it has stopped.
func (it *streamItem) stoppedAll() bool {
	for _, b := range it.blocks {
		if !b.stopped {
			return false
		}
	}
	return true
}

// content returns a message's parts, an output_text of each of its blocks' text so far, in
// order.
func (it *streamItem) content() []protocol.ContentPart {
	content := make([]protocol.ContentPart, len(it.blocks))
	for i, b := range it.blocks {
		content[i] = protocol.OutputText{Text: b.text.String()}
	}
	return content
}

// closeItem makes it's output_item.done, carrying the whole item, of status: the item
// DecodeResponse makes of the same blocks.
func (d *streamDecoder) closeItem(it *streamItem, status protocol.Status) error {
	first := it.blocks[0]
	switch first.typ {
	case "text":
		it.item = protocol.Message{ID: it.id, Status: status, Role: protocol.RoleAssistant, Content: it.content()}
	case "thinking", "redacted_thinking":
		r, err := decodeReasoning(block{Type: first.typ, Thinking: first.text.String(), Signature: first.signature, Data: first.data})
		if err != nil {
			return fmt.Errorf("reasoning: %w", err)
		}
		r.ID, r.Status = it.id, status
		it.item = r
	case "tool_use":
		it.item = protocol.FunctionCall{ID: it.id, Status: status, CallID: first.callID, Name: first.name, Arguments: first.text.String()}
	}
	it.done = true
	d.emit(protocol.OutputItemDone{SequenceNumber: d.next(), OutputIndex: it.index, Item: it.item})
	return nil
}

// End ends the stream, as NewStreamDecoder describes: it makes nothing once message_stop has
// made the terminal event, or an error frame or an error has ended it, and is
// io.ErrUnexpectedEOF before.
func (d *streamDecoder) End() ([]protocol.Event, error) {
	if d.ended {
		return nil, nil
	}
	d.ended = true
	return nil, fmt.Errorf("messages: stream: stream ended before message_stop: %w", io.ErrUnexpectedEOF)
}

// end reads message_stop, closing the stream with its terminal event.
func (d *streamDecoder) end() error {
	if !d.stopped {
		return errors.New("message_stop before message_delta")
	}
	d.ended = true
	resp := &protocol.Response{
		Output:            d.output(),
		Status:            d.status,
		IncompleteDetails: d.details,
		Usage:             decodeUsage(d.usage),
	}
	if d.status == protocol.StatusCompleted {
		d.emit(protocol.ResponseCompleted{SequenceNumber: d.next(), Response: resp})
	} else {
		d.emit(protocol.ResponseIncomplete{SequenceNumber: d.next(), Response: resp})
	}
	return nil
}

// fail closes the stream at an error frame, data, as NewStreamDecoder describes: the error
// event, then response.failed. anthropic is set for Anthropic's shape, whose error member holds
// the error; llama.cpp's holds it at the top level.
func (d *streamDecoder) fail(anthropic bool, data []byte) {
	d.ended = true
	d.create()
	var payload protocol.ErrorPayload
	if anthropic {
		payload.Message, payload.Type, payload.Code = format.DecodeOpenAIError(data)
	} else {
		// llama.cpp's body is the error object an OpenAI-style error body holds.
		wrapped := append(append([]byte(`{"error":`), data...), '}')
		payload.Message, payload.Type, payload.Code = format.DecodeOpenAIError(wrapped)
	}
	d.emit(protocol.ErrorEvent{SequenceNumber: d.next(), Error: payload})
	d.emit(protocol.ResponseFailed{SequenceNumber: d.next(), Response: &protocol.Response{
		Output: d.output(),
		Status: protocol.StatusFailed,
		Error:  &protocol.ResponseError{Code: payload.Code, Message: payload.Message},
		Usage:  decodeUsage(d.usage),
	}})
}

// output returns the items added, in output order: each done item as it was done, and each open
// one in progress with the content or the arguments made so far, as the Accumulator holds it: a
// reasoning item of its reasoning_text part, if it has one, without the encrypted_content its
// done event gives; a message of its parts; and a function call of its arguments.
func (d *streamDecoder) output() []protocol.Item {
	var out []protocol.Item
	for _, it := range d.items {
		if it.done {
			out = append(out, it.item)
			continue
		}
		first := it.blocks[0]
		switch first.typ {
		case "text":
			out = append(out, protocol.Message{ID: it.id, Status: protocol.StatusInProgress, Role: protocol.RoleAssistant, Content: it.content()})
		case "thinking":
			out = append(out, protocol.Reasoning{ID: it.id, Status: protocol.StatusInProgress, Content: []protocol.ReasoningText{{Text: first.text.String()}}})
		case "redacted_thinking":
			out = append(out, protocol.Reasoning{ID: it.id, Status: protocol.StatusInProgress})
		case "tool_use":
			out = append(out, protocol.FunctionCall{ID: it.id, Status: protocol.StatusInProgress, CallID: first.callID, Name: first.name, Arguments: first.text.String()})
		}
	}
	return out
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
