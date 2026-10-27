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
