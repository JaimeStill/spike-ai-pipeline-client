package responses_test

import (
	"testing"

	"github.com/JaimeStill/spike-ai-pipeline-client/format/responses"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// The tests here cover how a reasoning item goes back, in the spec's form and with
// ResendReasoningText.

// call is a function_call answering reasoning, on the wire as callJSON.
var call = protocol.FunctionCall{CallID: "call_1", Name: "get_weather", Arguments: `{"city":"Paris"}`}

const callJSON = `{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}`

// answer is an assistant message, on the wire as answerJSON.
var answer = protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "17"}}}

const answerJSON = `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"17"}]}`

// callOutputJSON is the function_call_output answering call on the wire.
const callOutputJSON = `{"type":"function_call_output","call_id":"call_1","output":"17"}`

var callOutput = protocol.FunctionCallOutput{CallID: "call_1", Output: "17"}

// reasoningCase is a request's input and the input array it goes out as.
type reasoningCase struct {
	name  string
	input []protocol.Item
	want  string
}

// runReasoning runs each case with the format adjusted by opts, wanting the request of the case's
// input, its input array the case's want.
func runReasoning(t *testing.T, opts responses.Options, cases []reasoningCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertJSON(t, encodeWith(t, opts, ask(tc.input...)), `{"model":"m",`+stateless+`,"input":`+tc.want+`}`)
		})
	}
}

// TestEncodeReasoningSpecForm covers the spec's form: a reasoning item goes back with its id, its
// summary, and the server's encrypted_content, never content, wherever it stands, and is dropped
// when that encrypted_content is missing or empty.
func TestEncodeReasoningSpecForm(t *testing.T) {
	user := text(protocol.RoleUser, "hi")
	cases := []reasoningCase{
		{
			name: "id, summary, and the server's encrypted_content; the texts and Content never sent",
			input: []protocol.Item{user, protocol.Reasoning{
				ID:               "rs_1",
				Status:           protocol.StatusCompleted,
				Content:          []protocol.ReasoningText{{Text: "never sent"}},
				Summary:          []protocol.SummaryText{{Text: "Looked."}, {Text: "Called."}},
				EncryptedContent: seal(t, "enc", "hm"),
			}, call},
			want: `[` + userHi + `,
				{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Looked."},{"type":"summary_text","text":"Called."}],
					"encrypted_content":"enc"},` + callJSON + `]`,
		},
		{
			// The envelope's literal, pinned: base64 of {"encrypted_content":"enc","content":["hm"]}.
			name: "no id and no summary, its summary an empty array",
			input: []protocol.Item{user, protocol.Reasoning{
				EncryptedContent: "spike-ai-pipeline:responses/v1:eyJlbmNyeXB0ZWRfY29udGVudCI6ImVuYyIsImNvbnRlbnQiOlsiaG0iXX0=",
			}, answer},
			want: `[` + userHi + `,{"type":"reasoning","summary":[],"encrypted_content":"enc"},` + answerJSON + `]`,
		},
		{
			name:  "lone reasoning goes back as it came",
			input: []protocol.Item{user, protocol.Reasoning{ID: "rs_1", EncryptedContent: seal(t, "enc")}},
			want:  `[` + userHi + `,{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"enc"}]`,
		},
		{
			name: "reasoning ahead of a user message goes back where it stands",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "enc")}, call, callOutput,
				protocol.Reasoning{EncryptedContent: seal(t, "enc2")}, text(protocol.RoleUser, "hi")},
			want: `[` + userHi + `,{"type":"reasoning","summary":[],"encrypted_content":"enc"},` + callJSON + `,` + callOutputJSON + `,
				{"type":"reasoning","summary":[],"encrypted_content":"enc2"},` + userHi + `]`,
		},
		{
			name:  "spec form drops the router's reasoning (empty encrypted_content)",
			input: []protocol.Item{user, protocol.Reasoning{ID: "rs_1", EncryptedContent: seal(t, "", "User asks.")}, call},
			want:  `[` + userHi + `,` + callJSON + `]`,
		},
	}
	cases = append(cases, dropCases(t, user)...)
	runReasoning(t, responses.Options{}, cases)
}

// TestEncodeReasoningResendText covers ResendReasoningText: a reasoning item goes back with its
// envelope's texts as content too, and a run of reasoning goes back only ahead of an assistant
// message or a function_call.
func TestEncodeReasoningResendText(t *testing.T) {
	user := text(protocol.RoleUser, "hi")
	cases := []reasoningCase{
		{
			// The router's reasoning: no encrypted_content to send back, the text as content.
			name:  "the router's reasoning ahead of a call: content, no encrypted_content",
			input: []protocol.Item{user, protocol.Reasoning{ID: "rs_1", EncryptedContent: seal(t, "", "User asks.")}, call},
			want: `[` + userHi + `,{"type":"reasoning","id":"rs_1","summary":[],
				"content":[{"type":"reasoning_text","text":"User asks."}]},` + callJSON + `]`,
		},
		{
			name: "texts and encrypted_content ahead of an assistant message, a part per text, the summary kept",
			input: []protocol.Item{user, protocol.Reasoning{
				Content:          []protocol.ReasoningText{{Text: "never sent"}},
				Summary:          []protocol.SummaryText{{Text: "Looked."}},
				EncryptedContent: seal(t, "enc", "hm", "ok"),
			}, answer},
			want: `[` + userHi + `,{"type":"reasoning","summary":[{"type":"summary_text","text":"Looked."}],
				"content":[{"type":"reasoning_text","text":"hm"},{"type":"reasoning_text","text":"ok"}],
				"encrypted_content":"enc"},` + answerJSON + `]`,
		},
		{
			name:  "encrypted_content alone, no content",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "enc")}, call},
			want:  `[` + userHi + `,{"type":"reasoning","summary":[],"encrypted_content":"enc"},` + callJSON + `]`,
		},
		{
			name: "a run of reasoning ahead of a call, a dropped item within it",
			input: []protocol.Item{user,
				protocol.Reasoning{EncryptedContent: seal(t, "", "a")},
				protocol.Reasoning{EncryptedContent: "foreign"},
				protocol.Reasoning{EncryptedContent: seal(t, "", "b")},
				call},
			want: `[` + userHi + `,
				{"type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"a"}]},
				{"type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"b"}]},` + callJSON + `]`,
		},
		{
			name:  "an envelope of neither text nor encrypted_content dropped",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "")}, call},
			want:  `[` + userHi + `,` + callJSON + `]`,
		},
		{
			name:  "lone reasoning dropped",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "enc", "hm")}},
			want:  `[` + userHi + `]`,
		},
		{
			name:  "reasoning ahead of a user message dropped",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "enc", "hm")}, user},
			want:  `[` + userHi + `,` + userHi + `]`,
		},
		{
			name:  "reasoning ahead of a function_call_output dropped",
			input: []protocol.Item{user, call, protocol.Reasoning{EncryptedContent: seal(t, "enc", "hm")}, callOutput},
			want:  `[` + userHi + `,` + callJSON + `,` + callOutputJSON + `]`,
		},
		{
			name: "reasoning ahead of a system message dropped",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "enc", "hm")},
				text(protocol.RoleSystem, "s")},
			want: `[` + userHi + `,{"type":"message","role":"system","content":[{"type":"input_text","text":"s"}]}]`,
		},
	}
	cases = append(cases, dropCases(t, user)...)
	runReasoning(t, responses.Options{ResendReasoningText: true}, cases)
}

// dropCases are the reasoning items both forms drop whatever follows them: those whose
// encrypted_content isn't the format's envelope.
func dropCases(t *testing.T, user protocol.Message) []reasoningCase {
	t.Helper()
	cases := []struct{ name, encrypted string }{
		{"no envelope dropped", ""},
		{"another format's envelope dropped", "spike-ai-pipeline:messages/v1:eyJkYXRhIjoieCJ9"},
		{"another version's envelope dropped", "spike-ai-pipeline:responses/v0:" + seal(t, "enc", "hm")[len("spike-ai-pipeline:responses/v1:"):]},
		{"an opaque server value dropped", "gAAAAABopaque"},
		{"an envelope not base64 dropped", "spike-ai-pipeline:responses/v1:not base64!"},
		// base64 of {"encrypted_content":"enc"}, no content member.
		{"an envelope of a member missing dropped", "spike-ai-pipeline:responses/v1:eyJlbmNyeXB0ZWRfY29udGVudCI6ImVuYyJ9"},
		// base64 of [1].
		{"an envelope not an object dropped", "spike-ai-pipeline:responses/v1:WzFd"},
	}
	out := make([]reasoningCase, 0, len(cases))
	for _, c := range cases {
		out = append(out, reasoningCase{
			name:  c.name,
			input: []protocol.Item{user, protocol.Reasoning{ID: "rs_1", EncryptedContent: c.encrypted}, call},
			want:  `[` + userHi + `,` + callJSON + `]`,
		})
	}
	return out
}

// TestReasoningRoundTripRecorded decodes the router's recorded reasoning and call, sends them
// back after the user's question with the call's output, and wants the request llama.cpp's
// router takes with ResendReasoningText: the reasoning's text as content ahead of the call, its
// id the router's, and no encrypted_content, the router's being empty. The spec's form drops the
// reasoning.
func TestReasoningRoundTripRecorded(t *testing.T) {
	resp, err := responses.New(responses.Options{}).DecodeResponse(readTestdata(t, "tool_call.json"))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	question := text(protocol.RoleUser, "What is the weather in Paris right now?")
	input := append(append([]protocol.Item{question}, resp.Output...),
		protocol.FunctionCallOutput{CallID: "call_HMhSOwdRkvjir1D59ezHFChk5pTnYdJc", Output: `{"celsius":17}`})
	questionJSON := `{"type":"message","role":"user","content":[{"type":"input_text","text":"What is the weather in Paris right now?"}]}`
	callJSON := `{"type":"function_call","id":"fc_HMhSOwdRkvjir1D59ezHFChk5pTnYdJc","call_id":"call_HMhSOwdRkvjir1D59ezHFChk5pTnYdJc",
		"name":"get_weather","arguments":"{\n  \"city\": \"Paris\"\n}"}`
	outputJSON := `{"type":"function_call_output","call_id":"call_HMhSOwdRkvjir1D59ezHFChk5pTnYdJc","output":"{\"celsius\":17}"}`

	assertJSON(t, encodeWith(t, responses.Options{ResendReasoningText: true}, ask(input...)), `{"model":"m",`+stateless+`,"input":[`+
		questionJSON+`,
		{"type":"reasoning","id":"rs_H4pfaCFvrGf1ljLr7M56OB81gR3sEvYK","summary":[],
			"content":[{"type":"reasoning_text","text":"User asks current weather in Paris. Use function get_weather."}]},`+
		callJSON+`,`+outputJSON+`]}`)
	assertJSON(t, encode(t, ask(input...)), `{"model":"m",`+stateless+`,"input":[`+questionJSON+`,`+callJSON+`,`+outputJSON+`]}`)
}
