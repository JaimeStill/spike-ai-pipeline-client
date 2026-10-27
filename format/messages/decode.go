package messages

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"

	"github.com/JaimeStill/spike-ai-pipeline-client/format"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// DecodeResponse decodes a Messages response body into output items in the order of its content
// blocks. Each run of consecutive text blocks becomes one assistant message, its content one
// output_text part per block, in order; any other block ends the run. Each output item gets the
// id itemID derives from the reply's id and the item's place in the output, as Messages supplies
// no item ids. The reply's stop_reason gives the response's status:
//
//	end_turn, tool_use, stop_sequence          completed
//	max_tokens, model_context_window_exceeded  incomplete, reason max_output_tokens
//	refusal                                    incomplete, reason content_filter
//
// The last output item takes the response's status, and the items before it are completed, as
// the spec has only the last item incomplete. pause_turn, which asks the caller to send the turn
// back for the endpoint to continue, has no place in a stateless response, and is an error, as is
// any other stop_reason.
//
// The format doesn't yet decode a thinking or redacted_thinking block, and drops it, so a
// reasoning model's reply still gives its answer; it makes no item, and ends a run of text. A
// block of any other type, tool_use among them, is an error.
//
// The reply's usage gives the response's: InputTokens is input_tokens with the prompt cache's
// cache_read_input_tokens and cache_creation_input_tokens added, as Messages counts those apart
// from input_tokens and the spec counts every input token; OutputTokens is output_tokens; and
// TotalTokens is their sum, which Messages doesn't report.
func (*Format) DecodeResponse(body []byte) (*protocol.Response, error) {
	var wire response
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("messages: decode response: %w", err)
	}
	resp := &protocol.Response{Usage: decodeUsage(wire.Usage)}
	var ok bool
	if resp.Status, resp.IncompleteDetails, ok = decodeStop(wire.StopReason); !ok {
		return nil, fmt.Errorf("messages: response has unknown stop_reason %q", wire.StopReason)
	}
	// text is the open run's message's content, nil when no run is open.
	var text []protocol.ContentPart
	closeRun := func() {
		if text == nil {
			return
		}
		resp.Output = append(resp.Output, protocol.Message{
			ID:      itemID(wire.ID, len(resp.Output)),
			Status:  protocol.StatusCompleted,
			Role:    protocol.RoleAssistant,
			Content: text,
		})
		text = nil
	}
	for i, b := range wire.Content {
		switch b.Type {
		case "text":
			text = append(text, protocol.OutputText{Text: b.Text})
		case "thinking", "redacted_thinking":
			closeRun()
		default:
			return nil, fmt.Errorf("messages: response content block %d has unknown type %q", i, b.Type)
		}
	}
	closeRun()
	if last := len(resp.Output) - 1; last >= 0 {
		resp.Output[last] = withStatus(resp.Output[last], resp.Status)
	}
	return resp, nil
}

// withStatus returns it, an output item, with status.
func withStatus(it protocol.Item, status protocol.Status) protocol.Item {
	if m, ok := it.(protocol.Message); ok {
		m.Status = status
		return m
	}
	return it
}

// itemID returns the id of the output item at output in the reply whose id is reply: the reply's
// id, a slash, and output in decimal, such as "msg_abc/1". Messages supplies no item ids, and the
// spec requires one on every output item, so the format derives them, as completions does. The id
// is unique within the reply, as output is, and across replies, as the reply's id is. A reply with
// no id gives its items none: an id made of output alone would repeat in every reply.
func itemID(reply string, output int) string {
	if reply == "" {
		return ""
	}
	return reply + "/" + strconv.Itoa(output)
}

// decodeUsage returns a reply's usage as the protocol's, as DecodeResponse describes.
func decodeUsage(u usage) protocol.Usage {
	input := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	return protocol.Usage{
		InputTokens:  input,
		OutputTokens: u.OutputTokens,
		TotalTokens:  input + u.OutputTokens,
	}
}

// decodeStop returns the status, and the incomplete details, that a stop_reason gives, as
// DecodeResponse describes, or false for a stop_reason it doesn't map.
func decodeStop(reason string) (protocol.Status, *protocol.IncompleteDetails, bool) {
	switch reason {
	case "end_turn", "tool_use", "stop_sequence":
		return protocol.StatusCompleted, nil, true
	case "max_tokens", "model_context_window_exceeded":
		return protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}, true
	case "refusal":
		return protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonContentFilter}, true
	}
	return "", nil, false
}

// DecodeError decodes an error body of either shape a Messages endpoint answers with:
// Anthropic's, {"type": "error", "error": {"type", "message"}}, and llama.cpp's OpenAI-style one,
// {"error": {"code", "message", "type"}}. Both keep the message and the type in an error object,
// so format.DecodeOpenAIError, which describes the shapes it reads, decodes either; Anthropic's
// gives no code, and its top-level type, always "error", isn't the error's.
func (*Format) DecodeError(body []byte) (message, typ, code string) {
	return format.DecodeOpenAIError(body)
}

// NewStreamDecoder returns a decoder that refuses every event: the format doesn't stream yet,
// and EncodeStreamRequest refuses every request, so the client never reaches it.
func (*Format) NewStreamDecoder() format.StreamDecoder {
	return unsupportedStream{}
}

// unsupportedStream is the decoder NewStreamDecoder returns, which refuses every event.
type unsupportedStream struct{}

// errNoStreaming is the error every unsupportedStream method returns.
var errNoStreaming = errors.New("messages: streaming is not yet supported")

// Decode refuses data.
func (unsupportedStream) Decode([]byte) ([]protocol.Event, error) {
	return nil, errNoStreaming
}

// End refuses to end the stream.
func (unsupportedStream) End() ([]protocol.Event, error) {
	return nil, errNoStreaming
}
