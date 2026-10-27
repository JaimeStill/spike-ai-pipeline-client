package protocol

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Accumulator folds a stream's events into the Response they describe, in the caller's own loop:
// Add takes each event in turn, and Response returns the Response folded so far. After a
// terminal event it is that event's Response; before one, it is the created Response holding the
// output so far, the items not yet done, and the parts not yet done, in StatusInProgress, so a
// stream that breaks leaves the partial output.
//
// Add refuses an event that doesn't follow the events before it: anything before
// response.created or after a terminal event, a sequence number that doesn't increase, an item
// added under the id of one added before it, an event about an item or part that wasn't added or
// is already done, an item_id that isn't the id of the item at the event's output_index, a part
// added before the one ahead of it is done, an item done before its parts, or a function call's
// arguments, are done, a done event whose text, arguments, or content isn't what the deltas made,
// and a terminal response.completed or response.incomplete while an item is open. A refused event
// changes nothing.
//
// It folds message items, of output_text and refusal parts, and function_call items, of argument
// deltas. Its zero value is ready to use; it isn't safe for concurrent use.
type Accumulator struct {
	created, ended bool
	// seq is the sequence number of the last event added.
	seq int
	// resp is the created response, and then the terminal one.
	resp Response
	// items are the output items added, in output order.
	items []*foldItem
}

// foldItem is an output item being folded.
type foldItem struct {
	// item is the item as added, and then as done.
	item Item
	done bool
	// parts are a message's content parts, in order.
	parts []*foldPart
	// args is a function call's arguments so far, and argsDone is set at their done event.
	args     strings.Builder
	argsDone bool
}

// foldPart is a content part being folded: an OutputText or a Refusal, as added, and its text
// so far.
type foldPart struct {
	part ContentPart
	text strings.Builder
	// textDone is set at the part's text's done event, and done at the part's.
	textDone, done bool
}

// Add folds e, the stream's next event, into the response, or refuses it, as Accumulator
// describes, changing nothing.
func (a *Accumulator) Add(e Event) error {
	if err := a.add(e); err != nil {
		return fmt.Errorf("protocol: accumulate: %w", err)
	}
	return nil
}

// add is Add without the package's prefix on its error.
func (a *Accumulator) add(e Event) error {
	name, seq, ok := describe(e)
	switch {
	case !ok:
		return fmt.Errorf("unsupported event %T", e)
	case a.ended:
		return fmt.Errorf("%s after the terminal event", name)
	case !a.created:
		if _, ok := e.(ResponseCreated); !ok {
			return fmt.Errorf("%s before response.created", name)
		}
	case seq <= a.seq:
		return fmt.Errorf("%s's sequence number %d doesn't follow %d", name, seq, a.seq)
	}
	if err := a.fold(e); err != nil {
		return fmt.Errorf("%s %d: %w", name, seq, err)
	}
	a.seq = seq
	return nil
}

// fold folds e, whose place in the stream add has checked, into the response.
func (a *Accumulator) fold(e Event) error {
	switch e := e.(type) {
	case ResponseCreated:
		switch {
		case a.created:
			return errors.New("the response was already created")
		case e.Response == nil:
			return errors.New("no response")
		case len(e.Response.Output) > 0:
			return errors.New("the created response has output")
		}
		a.resp, a.created = *e.Response, true
	case ResponseCompleted:
		return a.end(e.Response, true)
	case ResponseIncomplete:
		return a.end(e.Response, true)
	case ResponseFailed:
		return a.end(e.Response, false)
	case OutputItemAdded:
		return a.addItem(e)
	case OutputItemDone:
		return a.doneItem(e)
	case ContentPartAdded:
		return a.addPart(e)
	case ContentPartDone:
		p, err := a.part(e.ItemID, e.OutputIndex, e.ContentIndex)
		switch {
		case err != nil:
			return err
		case !p.textDone:
			return errors.New("the part's text isn't done")
		case !samePart(e.Part, p.folded()):
			return fmt.Errorf("part %#v isn't the part its deltas made, %#v", e.Part, p.folded())
		}
		p.done = true
	case OutputTextDelta:
		p, err := a.textPart(e.ItemID, e.OutputIndex, e.ContentIndex, false)
		if err != nil {
			return err
		}
		p.text.WriteString(e.Delta)
	case OutputTextDone:
		return a.doneText(e.ItemID, e.OutputIndex, e.ContentIndex, false, e.Text)
	case RefusalDelta:
		p, err := a.textPart(e.ItemID, e.OutputIndex, e.ContentIndex, true)
		if err != nil {
			return err
		}
		p.text.WriteString(e.Delta)
	case RefusalDone:
		return a.doneText(e.ItemID, e.OutputIndex, e.ContentIndex, true, e.Refusal)
	case FunctionCallArgumentsDelta:
		it, err := a.functionCall(e.ItemID, e.OutputIndex)
		if err != nil {
			return err
		}
		it.args.WriteString(e.Delta)
	case FunctionCallArgumentsDone:
		it, err := a.functionCall(e.ItemID, e.OutputIndex)
		switch {
		case err != nil:
			return err
		case e.Arguments != it.args.String():
			return fmt.Errorf("arguments %q aren't the arguments their deltas made, %q", e.Arguments, it.args.String())
		}
		it.argsDone = true
	}
	return nil
}

// end folds a terminal event's response r. A completed or incomplete response, closed, follows
// only once every item is done, and holds them all; a failed one may follow at any point.
func (a *Accumulator) end(r *Response, closed bool) error {
	if r == nil {
		return errors.New("no response")
	}
	if closed {
		for i, it := range a.items {
			if !it.done {
				return fmt.Errorf("output %d isn't done", i)
			}
		}
		if len(r.Output) != len(a.items) {
			return fmt.Errorf("the response has %d output items, want the %d streamed", len(r.Output), len(a.items))
		}
	}
	a.resp, a.ended = *r, true
	a.resp.Output = slices.Clone(r.Output)
	return nil
}

// addItem folds an output_item.added: the next item in output order, a message with no content
// or a function call with no arguments.
func (a *Accumulator) addItem(e OutputItemAdded) error {
	if e.OutputIndex != len(a.items) {
		return fmt.Errorf("output %d added, want output %d next", e.OutputIndex, len(a.items))
	}
	var id string
	switch it := e.Item.(type) {
	case Message:
		if len(it.Content) > 0 {
			return errors.New("the message is added with content")
		}
		id = it.ID
	case FunctionCall:
		if it.Arguments != "" {
			return errors.New("the function call is added with arguments")
		}
		id = it.ID
	default:
		return fmt.Errorf("unsupported item %T", e.Item)
	}
	// Items are found by output index, which every event about one carries, and their ids
	// checked against it; an empty id, from a format with none to give, is never a repeat.
	if id != "" {
		for i, it := range a.items {
			if itemID(it.item) == id {
				return fmt.Errorf("item %q is already output %d", id, i)
			}
		}
	}
	a.items = append(a.items, &foldItem{item: e.Item})
	return nil
}

// doneItem folds an output_item.done: the whole item, the one added there, a message's parts done
// and its content what they made, or a function call's arguments done and what their deltas
// made.
func (a *Accumulator) doneItem(e OutputItemDone) error {
	it, err := a.item(e.OutputIndex)
	if err != nil {
		return err
	}
	switch added := it.item.(type) {
	case Message:
		m, ok := e.Item.(Message)
		switch {
		case !ok:
			return fmt.Errorf("item %T is done where a message was added", e.Item)
		case m.ID != added.ID || m.Role != added.Role:
			return fmt.Errorf("message %q of role %s is done where message %q of role %s was added", m.ID, m.Role, added.ID, added.Role)
		case len(m.Content) != len(it.parts):
			return fmt.Errorf("the message has %d content parts, want the %d streamed", len(m.Content), len(it.parts))
		}
		for i, p := range it.parts {
			if !p.done {
				return fmt.Errorf("content %d isn't done", i)
			}
			if !samePart(m.Content[i], p.folded()) {
				return fmt.Errorf("content %d %#v isn't the part streamed, %#v", i, m.Content[i], p.folded())
			}
		}
	case FunctionCall:
		c, ok := e.Item.(FunctionCall)
		switch {
		case !ok:
			return fmt.Errorf("item %T is done where a function call was added", e.Item)
		case c.ID != added.ID || c.CallID != added.CallID || c.Name != added.Name:
			return fmt.Errorf("function call %q, call %q of %s, is done where function call %q, call %q of %s, was added", c.ID, c.CallID, c.Name, added.ID, added.CallID, added.Name)
		case !it.argsDone:
			return errors.New("the function call's arguments aren't done")
		case c.Arguments != it.args.String():
			return fmt.Errorf("arguments %q aren't the arguments streamed, %q", c.Arguments, it.args.String())
		}
	}
	it.item, it.done = e.Item, true
	return nil
}

// addPart folds a content_part.added: the item's next part, once the one ahead of it is done, an
// OutputText or a Refusal of a message.
func (a *Accumulator) addPart(e ContentPartAdded) error {
	it, err := a.message(e.ItemID, e.OutputIndex)
	if err != nil {
		return err
	}
	switch {
	case e.ContentIndex != len(it.parts):
		return fmt.Errorf("content %d added, want content %d next", e.ContentIndex, len(it.parts))
	case len(it.parts) > 0 && !it.parts[len(it.parts)-1].done:
		return fmt.Errorf("content %d isn't done", len(it.parts)-1)
	}
	p := &foldPart{part: e.Part}
	switch part := e.Part.(type) {
	case OutputText:
		p.text.WriteString(part.Text)
	case Refusal:
		p.text.WriteString(part.Refusal)
	default:
		return fmt.Errorf("unsupported part %T", e.Part)
	}
	it.parts = append(it.parts, p)
	return nil
}

// doneText folds an output_text.done, or a refusal.done when refusal is set: the part's whole
// text, which its deltas made.
func (a *Accumulator) doneText(itemID string, output, content int, refusal bool, text string) error {
	p, err := a.textPart(itemID, output, content, refusal)
	if err != nil {
		return err
	}
	if text != p.text.String() {
		return fmt.Errorf("text %q isn't the text its deltas made, %q", text, p.text.String())
	}
	p.textDone = true
	return nil
}

// item returns the output item at output, added and not done.
func (a *Accumulator) item(output int) (*foldItem, error) {
	switch {
	case output < 0 || output >= len(a.items):
		return nil, fmt.Errorf("output %d wasn't added", output)
	case a.items[output].done:
		return nil, fmt.Errorf("output %d is done", output)
	}
	return a.items[output], nil
}

// message returns the output item at output, added, not done, and a message whose id is
// itemID.
func (a *Accumulator) message(itemID string, output int) (*foldItem, error) {
	it, err := a.item(output)
	if err != nil {
		return nil, err
	}
	m, ok := it.item.(Message)
	switch {
	case !ok:
		return nil, fmt.Errorf("output %d is %T, not a message", output, it.item)
	case m.ID != itemID:
		return nil, fmt.Errorf("output %d is item %q, not %q", output, m.ID, itemID)
	}
	return it, nil
}

// functionCall returns the output item at output, added, not done, and a function call whose id
// is itemID, with its arguments still open.
func (a *Accumulator) functionCall(itemID string, output int) (*foldItem, error) {
	it, err := a.item(output)
	if err != nil {
		return nil, err
	}
	c, ok := it.item.(FunctionCall)
	switch {
	case !ok:
		return nil, fmt.Errorf("output %d is %T, not a function call", output, it.item)
	case c.ID != itemID:
		return nil, fmt.Errorf("output %d is item %q, not %q", output, c.ID, itemID)
	case it.argsDone:
		return nil, fmt.Errorf("output %d's arguments are done", output)
	}
	return it, nil
}

// part returns the content part at content of the message at output, added and not done.
func (a *Accumulator) part(itemID string, output, content int) (*foldPart, error) {
	it, err := a.message(itemID, output)
	if err != nil {
		return nil, err
	}
	switch {
	case content < 0 || content >= len(it.parts):
		return nil, fmt.Errorf("output %d content %d wasn't added", output, content)
	case it.parts[content].done:
		return nil, fmt.Errorf("output %d content %d is done", output, content)
	}
	return it.parts[content], nil
}

// textPart returns the part at content of the message at output, whose text is still open: a
// Refusal when refusal is set, and an OutputText otherwise.
func (a *Accumulator) textPart(itemID string, output, content int, refusal bool) (*foldPart, error) {
	p, err := a.part(itemID, output, content)
	if err != nil {
		return nil, err
	}
	if _, isRefusal := p.part.(Refusal); isRefusal != refusal {
		return nil, fmt.Errorf("output %d content %d is %T", output, content, p.part)
	}
	if p.textDone {
		return nil, fmt.Errorf("output %d content %d's text is done", output, content)
	}
	return p, nil
}

// itemID returns it's id, empty for an item the accumulator doesn't fold.
func itemID(it Item) string {
	switch it := it.(type) {
	case Message:
		return it.ID
	case FunctionCall:
		return it.ID
	}
	return ""
}

// folded is the part its events have made so far.
func (p *foldPart) folded() ContentPart {
	if _, ok := p.part.(Refusal); ok {
		return Refusal{Refusal: p.text.String()}
	}
	return OutputText{Text: p.text.String()}
}

// samePart reports whether got is want, an OutputText or a Refusal. It compares only those, so
// a part of another type, which may not be comparable, is never compared.
func samePart(got, want ContentPart) bool {
	switch got.(type) {
	case OutputText, Refusal:
		return got == want
	}
	return false
}

// Response returns the response folded so far, a new value each call. After a terminal event it
// is that event's Response. Before one it is the created Response, in StatusInProgress, holding
// the output so far: each item done as it was done, and each item still open in
// StatusInProgress with the content, or the arguments, its events have made. Before
// response.created it is empty.
func (a *Accumulator) Response() *Response {
	r := a.resp
	if a.ended {
		r.Output = slices.Clone(a.resp.Output)
		return &r
	}
	r.Output = nil
	for _, it := range a.items {
		if it.done {
			r.Output = append(r.Output, it.item)
			continue
		}
		switch item := it.item.(type) {
		case Message:
			item.Status = StatusInProgress
			item.Content = nil
			for _, p := range it.parts {
				item.Content = append(item.Content, p.folded())
			}
			r.Output = append(r.Output, item)
		case FunctionCall:
			item.Status = StatusInProgress
			item.Arguments = it.args.String()
			r.Output = append(r.Output, item)
		}
	}
	return &r
}

// describe returns e's type as the spec names it, and its sequence number, or false when e
// isn't one of the package's events by value.
func describe(e Event) (name string, seq int, ok bool) {
	switch e := e.(type) {
	case ResponseCreated:
		return "response.created", e.SequenceNumber, true
	case ResponseCompleted:
		return "response.completed", e.SequenceNumber, true
	case ResponseIncomplete:
		return "response.incomplete", e.SequenceNumber, true
	case ResponseFailed:
		return "response.failed", e.SequenceNumber, true
	case OutputItemAdded:
		return "response.output_item.added", e.SequenceNumber, true
	case OutputItemDone:
		return "response.output_item.done", e.SequenceNumber, true
	case ContentPartAdded:
		return "response.content_part.added", e.SequenceNumber, true
	case ContentPartDone:
		return "response.content_part.done", e.SequenceNumber, true
	case OutputTextDelta:
		return "response.output_text.delta", e.SequenceNumber, true
	case OutputTextDone:
		return "response.output_text.done", e.SequenceNumber, true
	case RefusalDelta:
		return "response.refusal.delta", e.SequenceNumber, true
	case RefusalDone:
		return "response.refusal.done", e.SequenceNumber, true
	case FunctionCallArgumentsDelta:
		return "response.function_call_arguments.delta", e.SequenceNumber, true
	case FunctionCallArgumentsDone:
		return "response.function_call_arguments.done", e.SequenceNumber, true
	}
	return "", 0, false
}
