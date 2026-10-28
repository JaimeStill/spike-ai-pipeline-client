package responses

import (
	"errors"

	"github.com/JaimeStill/spike-ai-pipeline-client/format"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// errNoStreaming is every error of this version's stream decoder.
var errNoStreaming = errors.New("responses: this version of the format doesn't decode a streamed reply")

// NewStreamDecoder returns a decoder for one streamed reply. This version of the format decodes
// none: the decoder refuses the stream's first event, and its end, so a streamed request fails
// rather than yield a stream it can't read.
func (*Format) NewStreamDecoder() format.StreamDecoder {
	return streamDecoder{}
}

// streamDecoder is this version's stream decoder, which refuses every stream.
type streamDecoder struct{}

// Decode refuses data, as NewStreamDecoder describes.
func (streamDecoder) Decode([]byte) ([]protocol.Event, error) {
	return nil, errNoStreaming
}

// End refuses the end of the stream, as NewStreamDecoder describes.
func (streamDecoder) End() ([]protocol.Event, error) {
	return nil, errNoStreaming
}
