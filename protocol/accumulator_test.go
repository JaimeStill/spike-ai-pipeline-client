package protocol_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// textStream is a whole stream of one assistant message, an output_text part of "Paris" in two
// deltas and a refusal part of "No.", ending with response.completed.
func textStream() []protocol.Event {
	msg := protocol.Message{
		Status: protocol.StatusCompleted,
		Role:   protocol.RoleAssistant,
		Content: []protocol.ContentPart{
			protocol.OutputText{Text: "Paris"},
			protocol.Refusal{Refusal: "No."},
		},
	}
	return []protocol.Event{
		protocol.ResponseCreated{SequenceNumber: 0, Response: &protocol.Response{Status: protocol.StatusInProgress}},
		protocol.OutputItemAdded{SequenceNumber: 1, Item: protocol.Message{Status: protocol.StatusInProgress, Role: protocol.RoleAssistant}},
		protocol.ContentPartAdded{SequenceNumber: 2, Part: protocol.OutputText{}},
		protocol.OutputTextDelta{SequenceNumber: 3, Delta: "Par"},
		protocol.OutputTextDelta{SequenceNumber: 4, Delta: "is"},
		protocol.OutputTextDone{SequenceNumber: 5, Text: "Paris"},
		protocol.ContentPartDone{SequenceNumber: 6, Part: protocol.OutputText{Text: "Paris"}},
		protocol.ContentPartAdded{SequenceNumber: 7, ContentIndex: 1, Part: protocol.Refusal{}},
		protocol.RefusalDelta{SequenceNumber: 8, ContentIndex: 1, Delta: "No."},
		protocol.RefusalDone{SequenceNumber: 9, ContentIndex: 1, Refusal: "No."},
		protocol.ContentPartDone{SequenceNumber: 10, ContentIndex: 1, Part: protocol.Refusal{Refusal: "No."}},
		protocol.OutputItemDone{SequenceNumber: 11, Item: msg},
		protocol.ResponseCompleted{SequenceNumber: 12, Response: &protocol.Response{
			Output: []protocol.Item{msg},
			Status: protocol.StatusCompleted,
			Usage:  protocol.Usage{InputTokens: 7, OutputTokens: 5, TotalTokens: 12},
		}},
	}
}

// add folds events, failing t if the Accumulator refuses one.
func add(t *testing.T, acc *protocol.Accumulator, events []protocol.Event) {
	t.Helper()
	for i, e := range events {
		if err := acc.Add(e); err != nil {
			t.Fatalf("Add(event %d): %v", i, err)
		}
	}
}

func TestAccumulatorFoldsTerminalResponse(t *testing.T) {
	events := textStream()
	var acc protocol.Accumulator
	add(t, &acc, events)
	want := events[len(events)-1].(protocol.ResponseCompleted).Response
	got := acc.Response()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Response = %#v\nwant       %#v", got, want)
	}
	if got == want {
		t.Error("Response is the event's own *Response, want a new value")
	}
}

// TestAccumulatorPartial wants the response folded so far before the terminal event: done
// parts and items as done, and open ones in progress with their text so far.
func TestAccumulatorPartial(t *testing.T) {
	inProgress := func(content ...protocol.ContentPart) *protocol.Response {
		return &protocol.Response{Status: protocol.StatusInProgress, Output: []protocol.Item{protocol.Message{
			Status:  protocol.StatusInProgress,
			Role:    protocol.RoleAssistant,
			Content: content,
		}}}
	}
	cases := []struct {
		events int
		want   *protocol.Response
	}{
		{0, &protocol.Response{}},
		{1, &protocol.Response{Status: protocol.StatusInProgress}},
		{2, inProgress()},
		{4, inProgress(protocol.OutputText{Text: "Par"})},
		{7, inProgress(protocol.OutputText{Text: "Paris"})},
		{9, inProgress(protocol.OutputText{Text: "Paris"}, protocol.Refusal{Refusal: "No."})},
		{12, &protocol.Response{Status: protocol.StatusInProgress, Output: []protocol.Item{textStream()[11].(protocol.OutputItemDone).Item}}},
	}
	for _, tc := range cases {
		var acc protocol.Accumulator
		add(t, &acc, textStream()[:tc.events])
		if got := acc.Response(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("after %d events, Response = %#v\nwant %#v", tc.events, got, tc.want)
		}
	}
}

// TestAccumulatorRefusesEvent covers the events that don't follow the events before them: each
// case folds the first n events of textStream, then wants e refused, and the response
// unchanged.
func TestAccumulatorRefusesEvent(t *testing.T) {
	message := protocol.Message{Status: protocol.StatusInProgress, Role: protocol.RoleAssistant}
	done := textStream()[11].(protocol.OutputItemDone).Item
	completed := func(seq int, output ...protocol.Item) protocol.Event {
		return protocol.ResponseCompleted{SequenceNumber: seq, Response: &protocol.Response{Output: output, Status: protocol.StatusCompleted}}
	}
	cases := []struct {
		name string
		n    int
		e    protocol.Event
		want string
	}{
		{"nil", 0, nil, "unsupported event <nil>"},
		{"a pointer", 0, &protocol.ResponseCreated{}, "unsupported event *protocol.ResponseCreated"},
		{"an event before response.created", 0, protocol.OutputItemAdded{Item: message}, "before response.created"},
		{"response.created twice", 1, protocol.ResponseCreated{SequenceNumber: 1, Response: &protocol.Response{}}, "already created"},
		{"response.created with no response", 0, protocol.ResponseCreated{}, "no response"},
		{"response.created with output", 0, protocol.ResponseCreated{Response: &protocol.Response{Output: []protocol.Item{message}}}, "has output"},
		{"a repeated sequence number", 2, protocol.OutputTextDelta{SequenceNumber: 1, Delta: "x"}, "sequence number 1 doesn't follow 1"},
		{"an item out of output order", 1, protocol.OutputItemAdded{SequenceNumber: 1, OutputIndex: 1, Item: message}, "output 1 added, want output 0 next"},
		{"an item added with content", 1, protocol.OutputItemAdded{SequenceNumber: 1, Item: done}, "added with content"},
		{"an item of an unsupported type", 1, protocol.OutputItemAdded{SequenceNumber: 1, Item: protocol.FunctionCallOutput{}}, "unsupported item"},
		{"a part of an item not added", 1, protocol.ContentPartAdded{SequenceNumber: 1, Part: protocol.OutputText{}}, "output 0 wasn't added"},
		{"a part of another item id", 2, protocol.ContentPartAdded{SequenceNumber: 2, ItemID: "msg_1", Part: protocol.OutputText{}}, `not "msg_1"`},
		{"a part out of content order", 2, protocol.ContentPartAdded{SequenceNumber: 2, ContentIndex: 1, Part: protocol.OutputText{}}, "content 1 added, want content 0 next"},
		{"a part of an unsupported type", 2, protocol.ContentPartAdded{SequenceNumber: 2, Part: protocol.InputText{}}, "unsupported part"},
		{"a part added before the one ahead is done", 4, protocol.ContentPartAdded{SequenceNumber: 4, ContentIndex: 1, Part: protocol.Refusal{}}, "content 0 isn't done"},
		{"a delta of a part not added", 2, protocol.OutputTextDelta{SequenceNumber: 2, Delta: "x"}, "content 0 wasn't added"},
		{"an arguments delta of a message", 2, protocol.FunctionCallArgumentsDelta{SequenceNumber: 2, Delta: "x"}, "is protocol.Message, not a function call"},
		{"a refusal delta of an output_text part", 3, protocol.RefusalDelta{SequenceNumber: 3, Delta: "x"}, "is protocol.OutputText"},
		{"a text delta of a refusal part", 8, protocol.OutputTextDelta{SequenceNumber: 8, ContentIndex: 1, Delta: "x"}, "is protocol.Refusal"},
		{"a delta after the text is done", 6, protocol.OutputTextDelta{SequenceNumber: 6, Delta: "x"}, "text is done"},
		{"a done text the deltas didn't make", 5, protocol.OutputTextDone{SequenceNumber: 5, Text: "Pari"}, "isn't the text its deltas made"},
		{"a part done before its text", 5, protocol.ContentPartDone{SequenceNumber: 5, Part: protocol.OutputText{Text: "Paris"}}, "text isn't done"},
		{"a done part the deltas didn't make", 6, protocol.ContentPartDone{SequenceNumber: 6, Part: protocol.OutputText{Text: "Pari"}}, "isn't the part its deltas made"},
		{"a delta of a done part", 7, protocol.OutputTextDelta{SequenceNumber: 7, Delta: "x"}, "content 0 is done"},
		{"an item done with a part open", 9, protocol.OutputItemDone{SequenceNumber: 9, Item: done}, "content 1 isn't done"},
		{"an item done of another type", 11, protocol.OutputItemDone{SequenceNumber: 11, Item: protocol.FunctionCall{}}, "where a message was added"},
		{"an item done of other content", 11, protocol.OutputItemDone{SequenceNumber: 11, Item: protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}, protocol.Refusal{Refusal: "Yes."}}}}, "isn't the part streamed"},
		{"an item done twice", 12, protocol.OutputItemDone{SequenceNumber: 12, Item: done}, "output 0 is done"},
		{"response.completed with an item open", 11, completed(11), "output 0 isn't done"},
		{"response.completed missing an item", 12, completed(12), "has 0 output items, want the 1 streamed"},
		{"response.completed with no response", 12, protocol.ResponseCompleted{SequenceNumber: 12}, "no response"},
		{"an event after the terminal event", 13, protocol.ResponseCompleted{SequenceNumber: 13, Response: &protocol.Response{}}, "after the terminal event"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var acc protocol.Accumulator
			add(t, &acc, textStream()[:tc.n])
			before := acc.Response()
			err := acc.Add(tc.e)
			if err == nil {
				t.Fatalf("Add(%#v) = nil, want an error containing %q", tc.e, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Add error = %q, want it to contain %q", err, tc.want)
			}
			if after := acc.Response(); !reflect.DeepEqual(after, before) {
				t.Errorf("Response after a refused event = %#v, want it unchanged, %#v", after, before)
			}
		})
	}
}

// TestAccumulatorFailedWithItemOpen wants response.failed to follow an item still open, unlike
// response.completed: a stream may fail part way.
func TestAccumulatorFailedWithItemOpen(t *testing.T) {
	var acc protocol.Accumulator
	add(t, &acc, textStream()[:4])
	failed := &protocol.Response{Usage: protocol.Usage{InputTokens: 7, TotalTokens: 7}}
	if err := acc.Add(protocol.ResponseFailed{SequenceNumber: 4, Response: failed}); err != nil {
		t.Fatalf("Add(response.failed): %v", err)
	}
	if got := acc.Response(); !reflect.DeepEqual(got, failed) {
		t.Errorf("Response = %#v, want the failed response %#v", got, failed)
	}
}

// TestAccumulatorItemIDs wants an item added under the id of an item before it refused, and
// items of no id, from a format with none to give, accepted however many there are.
func TestAccumulatorItemIDs(t *testing.T) {
	// items folds a stream of empty assistant messages of ids, each added and done, then adds
	// one more of id last, and returns Add's error for it.
	items := func(last string, ids ...string) error {
		var acc protocol.Accumulator
		events := []protocol.Event{protocol.ResponseCreated{Response: &protocol.Response{}}}
		for i, id := range ids {
			m := protocol.Message{ID: id, Role: protocol.RoleAssistant}
			events = append(events,
				protocol.OutputItemAdded{SequenceNumber: 2*i + 1, OutputIndex: i, Item: m},
				protocol.OutputItemDone{SequenceNumber: 2*i + 2, OutputIndex: i, Item: m})
		}
		add(t, &acc, events)
		return acc.Add(protocol.OutputItemAdded{SequenceNumber: len(events), OutputIndex: len(ids), Item: protocol.Message{ID: last, Role: protocol.RoleAssistant}})
	}
	if err := items("r/1", "r/0"); err != nil {
		t.Errorf("Add(a new id) = %v, want nil", err)
	}
	if err := items("", "", ""); err != nil {
		t.Errorf("Add(no id, after items of none) = %v, want nil", err)
	}
	if err := items("r/0", "r/0", "r/1"); err == nil || !strings.Contains(err.Error(), `item "r/0" is already output 0`) {
		t.Errorf("Add(a repeated id) = %v, want an error naming the earlier output", err)
	}
}

// callStream is a whole stream of one function call, its arguments `{"city":"Paris"}` in two
// deltas, ending with response.completed.
func callStream() []protocol.Event {
	added := protocol.FunctionCall{ID: "r/0", Status: protocol.StatusInProgress, CallID: "c1", Name: "get_weather"}
	call := protocol.FunctionCall{ID: "r/0", Status: protocol.StatusCompleted, CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`}
	return []protocol.Event{
		protocol.ResponseCreated{SequenceNumber: 0, Response: &protocol.Response{Status: protocol.StatusInProgress}},
		protocol.OutputItemAdded{SequenceNumber: 1, Item: added},
		protocol.FunctionCallArgumentsDelta{SequenceNumber: 2, ItemID: "r/0", Delta: `{"city":`},
		protocol.FunctionCallArgumentsDelta{SequenceNumber: 3, ItemID: "r/0", Delta: `"Paris"}`},
		protocol.FunctionCallArgumentsDone{SequenceNumber: 4, ItemID: "r/0", Arguments: `{"city":"Paris"}`},
		protocol.OutputItemDone{SequenceNumber: 5, Item: call},
		protocol.ResponseCompleted{SequenceNumber: 6, Response: &protocol.Response{
			Output: []protocol.Item{call},
			Status: protocol.StatusCompleted,
		}},
	}
}

// TestAccumulatorFunctionCall wants a function call folded: open, in progress with the arguments
// its deltas have made, and done, the call as done, and after the terminal event that event's
// response.
func TestAccumulatorFunctionCall(t *testing.T) {
	inProgress := func(arguments string) *protocol.Response {
		return &protocol.Response{Status: protocol.StatusInProgress, Output: []protocol.Item{protocol.FunctionCall{
			ID: "r/0", Status: protocol.StatusInProgress, CallID: "c1", Name: "get_weather", Arguments: arguments,
		}}}
	}
	events := callStream()
	cases := []struct {
		events int
		want   *protocol.Response
	}{
		{2, inProgress("")},
		{3, inProgress(`{"city":`)},
		{5, inProgress(`{"city":"Paris"}`)},
		{6, &protocol.Response{Status: protocol.StatusInProgress, Output: []protocol.Item{events[5].(protocol.OutputItemDone).Item}}},
		{7, events[6].(protocol.ResponseCompleted).Response},
	}
	for _, tc := range cases {
		var acc protocol.Accumulator
		add(t, &acc, callStream()[:tc.events])
		if got := acc.Response(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("after %d events, Response = %#v\nwant %#v", tc.events, got, tc.want)
		}
	}
}

// TestAccumulatorRefusesArgumentsEvent covers the function call events that don't follow the
// events before them: arguments events out of order, of another item, and arguments the deltas
// didn't make. Each case folds the first n events of callStream, then wants e refused, and the
// response unchanged.
func TestAccumulatorRefusesArgumentsEvent(t *testing.T) {
	call := callStream()[5].(protocol.OutputItemDone).Item.(protocol.FunctionCall)
	with := func(edit func(*protocol.FunctionCall)) protocol.FunctionCall {
		c := call
		edit(&c)
		return c
	}
	cases := []struct {
		name string
		n    int
		e    protocol.Event
		want string
	}{
		{"a call added with arguments", 1, protocol.OutputItemAdded{SequenceNumber: 1, Item: call}, "added with arguments"},
		{"a delta of a call not added", 1, protocol.FunctionCallArgumentsDelta{SequenceNumber: 1, ItemID: "r/0", Delta: "{"}, "output 0 wasn't added"},
		{"a done of a call not added", 1, protocol.FunctionCallArgumentsDone{SequenceNumber: 1, ItemID: "r/0"}, "output 0 wasn't added"},
		{"a delta of another item id", 2, protocol.FunctionCallArgumentsDelta{SequenceNumber: 2, ItemID: "r/1", Delta: "{"}, `output 0 is item "r/0", not "r/1"`},
		{"a content part of a call", 2, protocol.ContentPartAdded{SequenceNumber: 2, ItemID: "r/0", Part: protocol.OutputText{}}, "is protocol.FunctionCall, not a message"},
		{"a done the deltas didn't make", 4, protocol.FunctionCallArgumentsDone{SequenceNumber: 4, ItemID: "r/0", Arguments: `{"city":"Lyon"}`}, "aren't the arguments their deltas made"},
		{"a delta after the arguments are done", 5, protocol.FunctionCallArgumentsDelta{SequenceNumber: 5, ItemID: "r/0", Delta: " "}, "arguments are done"},
		{"a done twice", 5, protocol.FunctionCallArgumentsDone{SequenceNumber: 5, ItemID: "r/0", Arguments: `{"city":"Paris"}`}, "arguments are done"},
		{"a call done before its arguments", 4, protocol.OutputItemDone{SequenceNumber: 4, Item: call}, "arguments aren't done"},
		{"a call done of other arguments", 5, protocol.OutputItemDone{SequenceNumber: 5, Item: with(func(c *protocol.FunctionCall) { c.Arguments = "{}" })}, "aren't the arguments streamed"},
		{"a call done of another call id", 5, protocol.OutputItemDone{SequenceNumber: 5, Item: with(func(c *protocol.FunctionCall) { c.CallID = "c2" })}, "was added"},
		{"a call done of another name", 5, protocol.OutputItemDone{SequenceNumber: 5, Item: with(func(c *protocol.FunctionCall) { c.Name = "get_time" })}, "was added"},
		{"a call done of another type", 5, protocol.OutputItemDone{SequenceNumber: 5, Item: protocol.Message{ID: "r/0"}}, "where a function call was added"},
		{"a delta after the call is done", 6, protocol.FunctionCallArgumentsDelta{SequenceNumber: 6, ItemID: "r/0", Delta: " "}, "output 0 is done"},
		{"response.completed with the call open", 5, protocol.ResponseCompleted{SequenceNumber: 5, Response: &protocol.Response{Output: []protocol.Item{call}}}, "output 0 isn't done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var acc protocol.Accumulator
			add(t, &acc, callStream()[:tc.n])
			before := acc.Response()
			err := acc.Add(tc.e)
			if err == nil {
				t.Fatalf("Add(%#v) = nil, want an error containing %q", tc.e, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Add error = %q, want it to contain %q", err, tc.want)
			}
			if after := acc.Response(); !reflect.DeepEqual(after, before) {
				t.Errorf("Response after a refused event = %#v, want it unchanged, %#v", after, before)
			}
		})
	}
}

// reasoningStream is a whole stream of a reasoning item, a reasoning_text part of "Hm, Paris." in
// two deltas, then a message of "Paris", ending with response.completed.
func reasoningStream() []protocol.Event {
	reasoning := protocol.Reasoning{ID: "r/0", Status: protocol.StatusCompleted, Content: []protocol.ReasoningText{{Text: "Hm, Paris."}}, EncryptedContent: "sealed"}
	msg := protocol.Message{ID: "r/1", Status: protocol.StatusCompleted, Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}}}
	return []protocol.Event{
		protocol.ResponseCreated{SequenceNumber: 0, Response: &protocol.Response{Status: protocol.StatusInProgress}},
		protocol.OutputItemAdded{SequenceNumber: 1, Item: protocol.Reasoning{ID: "r/0", Status: protocol.StatusInProgress}},
		protocol.ContentPartAdded{SequenceNumber: 2, ItemID: "r/0", Part: protocol.ReasoningText{}},
		protocol.ReasoningDelta{SequenceNumber: 3, ItemID: "r/0", Delta: "Hm,"},
		protocol.ReasoningDelta{SequenceNumber: 4, ItemID: "r/0", Delta: " Paris."},
		protocol.ReasoningDone{SequenceNumber: 5, ItemID: "r/0", Text: "Hm, Paris."},
		protocol.ContentPartDone{SequenceNumber: 6, ItemID: "r/0", Part: protocol.ReasoningText{Text: "Hm, Paris."}},
		protocol.OutputItemDone{SequenceNumber: 7, Item: reasoning},
		protocol.OutputItemAdded{SequenceNumber: 8, OutputIndex: 1, Item: protocol.Message{ID: "r/1", Status: protocol.StatusInProgress, Role: protocol.RoleAssistant}},
		protocol.ContentPartAdded{SequenceNumber: 9, ItemID: "r/1", OutputIndex: 1, Part: protocol.OutputText{}},
		protocol.OutputTextDelta{SequenceNumber: 10, ItemID: "r/1", OutputIndex: 1, Delta: "Paris"},
		protocol.OutputTextDone{SequenceNumber: 11, ItemID: "r/1", OutputIndex: 1, Text: "Paris"},
		protocol.ContentPartDone{SequenceNumber: 12, ItemID: "r/1", OutputIndex: 1, Part: protocol.OutputText{Text: "Paris"}},
		protocol.OutputItemDone{SequenceNumber: 13, OutputIndex: 1, Item: msg},
		protocol.ResponseCompleted{SequenceNumber: 14, Response: &protocol.Response{
			Output: []protocol.Item{reasoning, msg},
			Status: protocol.StatusCompleted,
		}},
	}
}

// TestAccumulatorReasoning wants a reasoning item folded: open, in progress, with the reasoning
// its deltas have made and no encrypted content, which only its done event gives; done, the item
// as done, its status and encrypted content included; and ahead of the message after it, in
// output order.
func TestAccumulatorReasoning(t *testing.T) {
	inProgress := func(output ...protocol.Item) *protocol.Response {
		return &protocol.Response{Status: protocol.StatusInProgress, Output: output}
	}
	open := func(content ...protocol.ReasoningText) protocol.Reasoning {
		return protocol.Reasoning{ID: "r/0", Status: protocol.StatusInProgress, Content: content}
	}
	events := reasoningStream()
	done := events[7].(protocol.OutputItemDone).Item
	cases := []struct {
		events int
		want   *protocol.Response
	}{
		{2, inProgress(open())},
		{3, inProgress(open(protocol.ReasoningText{}))},
		{4, inProgress(open(protocol.ReasoningText{Text: "Hm,"}))},
		{7, inProgress(open(protocol.ReasoningText{Text: "Hm, Paris."}))},
		{8, inProgress(done)},
		{11, inProgress(done, protocol.Message{ID: "r/1", Status: protocol.StatusInProgress, Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Paris"}}})},
		{15, events[14].(protocol.ResponseCompleted).Response},
	}
	for _, tc := range cases {
		var acc protocol.Accumulator
		add(t, &acc, reasoningStream()[:tc.events])
		if got := acc.Response(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("after %d events, Response = %#v\nwant %#v", tc.events, got, tc.want)
		}
	}
}

// TestAccumulatorRefusesReasoningEvent covers the reasoning events that don't follow the events
// before them: out of order, of another item or part, and text the deltas didn't make. Each case
// folds the first n events of reasoningStream, then wants e refused, and the response unchanged.
func TestAccumulatorRefusesReasoningEvent(t *testing.T) {
	reasoning := reasoningStream()[7].(protocol.OutputItemDone).Item.(protocol.Reasoning)
	cases := []struct {
		name string
		n    int
		e    protocol.Event
		want string
	}{
		{"a reasoning item added with content", 1, protocol.OutputItemAdded{SequenceNumber: 1, Item: protocol.Reasoning{ID: "r/0", Content: reasoning.Content}}, "added with content"},
		{"a reasoning item added with encrypted content", 1, protocol.OutputItemAdded{SequenceNumber: 1, Item: protocol.Reasoning{ID: "r/0", EncryptedContent: "sealed"}}, "added with content"},
		{"a delta before the part is added", 2, protocol.ReasoningDelta{SequenceNumber: 2, ItemID: "r/0", Delta: "Hm"}, "content 0 wasn't added"},
		{"an output_text part of a reasoning item", 2, protocol.ContentPartAdded{SequenceNumber: 2, ItemID: "r/0", Part: protocol.OutputText{}}, "unsupported part protocol.OutputText of a reasoning item"},
		{"a reasoning_text part of a message", 9, protocol.ContentPartAdded{SequenceNumber: 9, ItemID: "r/1", OutputIndex: 1, Part: protocol.ReasoningText{}}, "unsupported part protocol.ReasoningText of a message"},
		{"a reasoning delta of another item id", 3, protocol.ReasoningDelta{SequenceNumber: 3, ItemID: "r/1", Delta: "Hm"}, `output 0 is item "r/0", not "r/1"`},
		{"an output_text delta of a reasoning_text part", 3, protocol.OutputTextDelta{SequenceNumber: 3, ItemID: "r/0", Delta: "Hm"}, "is protocol.ReasoningText"},
		{"a reasoning delta of an output_text part", 10, protocol.ReasoningDelta{SequenceNumber: 10, ItemID: "r/1", OutputIndex: 1, Delta: "Hm"}, "is protocol.OutputText"},
		{"a done text the deltas didn't make", 5, protocol.ReasoningDone{SequenceNumber: 5, ItemID: "r/0", Text: "Hm, Lyon."}, "isn't the text its deltas made"},
		{"a delta after the text is done", 6, protocol.ReasoningDelta{SequenceNumber: 6, ItemID: "r/0", Delta: " "}, "text is done"},
		{"a part done before its text", 5, protocol.ContentPartDone{SequenceNumber: 5, ItemID: "r/0", Part: protocol.ReasoningText{Text: "Hm, Paris."}}, "text isn't done"},
		{"a done part the deltas didn't make", 6, protocol.ContentPartDone{SequenceNumber: 6, ItemID: "r/0", Part: protocol.ReasoningText{Text: "Hm."}}, "isn't the part its deltas made"},
		{"a reasoning item done with its part open", 6, protocol.OutputItemDone{SequenceNumber: 6, Item: reasoning}, "content 0 isn't done"},
		{"a reasoning item done of other content", 7, protocol.OutputItemDone{SequenceNumber: 7, Item: protocol.Reasoning{ID: "r/0", Content: []protocol.ReasoningText{{Text: "Hm."}}}}, "isn't the part streamed"},
		{"a reasoning item done of no content", 7, protocol.OutputItemDone{SequenceNumber: 7, Item: protocol.Reasoning{ID: "r/0"}}, "has 0 content parts, want the 1 streamed"},
		{"a reasoning item done of another id", 7, protocol.OutputItemDone{SequenceNumber: 7, Item: protocol.Reasoning{ID: "r/9", Content: reasoning.Content}}, `reasoning item "r/9" is done where reasoning item "r/0" was added`},
		{"a reasoning item done as a message", 7, protocol.OutputItemDone{SequenceNumber: 7, Item: protocol.Message{ID: "r/0"}}, "where a reasoning item was added"},
		{"a function call done where reasoning was added", 7, protocol.OutputItemDone{SequenceNumber: 7, Item: protocol.FunctionCall{ID: "r/0"}}, "where a reasoning item was added"},
		{"a message added at the reasoning item's output index", 7, protocol.OutputItemAdded{SequenceNumber: 7, OutputIndex: 0, Item: protocol.Message{ID: "r/1", Role: protocol.RoleAssistant}}, "output 0 added, want output 1 next"},
		{"response.completed with the reasoning item open", 7, protocol.ResponseCompleted{SequenceNumber: 7, Response: &protocol.Response{Output: []protocol.Item{reasoning}}}, "output 0 isn't done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var acc protocol.Accumulator
			add(t, &acc, reasoningStream()[:tc.n])
			before := acc.Response()
			err := acc.Add(tc.e)
			if err == nil {
				t.Fatalf("Add(%#v) = nil, want an error containing %q", tc.e, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Add error = %q, want it to contain %q", err, tc.want)
			}
			if after := acc.Response(); !reflect.DeepEqual(after, before) {
				t.Errorf("Response after a refused event = %#v, want it unchanged, %#v", after, before)
			}
		})
	}
}
