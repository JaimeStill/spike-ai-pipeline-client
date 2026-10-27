package client

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/url"
	"slices"

	"github.com/JaimeStill/spike-ai-pipeline-client/format"
)

// transcriptionsRoute is the OpenAI-compatible transcription endpoint, relative to the base URL.
const transcriptionsRoute = "audio/transcriptions"

// TranscriptionRequest asks an audio model for the text of a clip. Transcription sits outside
// the protocol and the Format: it goes to the OpenAI-compatible transcription endpoint whatever
// format the Client speaks.
//
// The endpoint's other form fields are not typed. response_format is always json, the one
// llama.cpp b11669 answers (it refuses text, srt, verbose_json, and vtt), so the reply carries
// no timestamps, and timestamp_granularities, which only verbose_json honors, has nothing to
// shape. temperature and max_tokens, which llama.cpp passes to its chat completion, and stream,
// which changes the reply's shape, are left to Native.
type TranscriptionRequest struct {
	// Model is the audio model's id. It is required.
	Model string
	// File is the clip, read once, as it is uploaded, and never held whole in memory. It is
	// required. Transcribe neither closes it nor reads it after returning.
	File io.Reader
	// FileName names the clip in the upload, such as "clip.wav". It is required: the endpoint
	// takes the clip as a file, and a file part has a name.
	FileName string
	// Language is the clip's language, such as "en", when the caller knows it; empty sends none.
	// llama.cpp b11669 adds it to its transcription instruction as a hint.
	Language string
	// Prompt guides the transcription; empty sends none. Its effect is the endpoint's: OpenAI
	// reads it as preceding context, while llama.cpp b11669 sends it to the model in place of its
	// own transcription instruction.
	Prompt string
	// Native is the native tier: form fields sent as they are beside the typed ones, for a
	// feature of the endpoint the typed fields don't cover, such as temperature. Each key goes
	// out as a field once per value, keys in sorted order and each key's values in order, after
	// the typed fields and before the file. Its fields mean what the endpoint makes them mean;
	// one that changes the reply's shape, such as stream, makes a reply Transcribe can't decode.
	// A key that clashes with a field the typed fields set is refused: model, response_format,
	// and file always, and language and prompt when Language and Prompt are set. So is an empty
	// key, which names no field.
	Native url.Values
}

// Transcription is an audio model's transcript of a clip.
type Transcription struct {
	// Text is the clip's transcript.
	Text string
	// Usage counts the tokens the request took.
	Usage TranscriptionUsage
}

// TranscriptionUsage counts the tokens a transcription took, in the endpoint's names, which are
// protocol.Usage's: the clip and the instruction are input, the transcript output.
type TranscriptionUsage struct {
	// InputTokens is the tokens of the clip and the transcription instruction.
	InputTokens int
	// OutputTokens is the tokens of the transcript.
	OutputTokens int
	// TotalTokens is input and output tokens together.
	TotalTokens int
}

// Transcribe uploads req's clip to the endpoint's transcription route and returns its
// transcript. It refuses a nil request, one with no model, file, or file name, and a Native key
// that is empty or clashes with a typed field, before the token source is called or anything is
// sent.
//
// The upload streams: a goroutine writes the multipart form (the model, response_format json,
// the language and prompt when set, Native's fields, then the file) into a pipe the request
// reads, so the clip is read as it is sent, never held whole, and goes chunked, its length
// unknown. Transcribe returns only once that goroutine has ended, so it has stopped reading
// File: when the request ends first (a cancelled ctx, a transport failure, an early reply), the
// pipe is closed and the goroutine's next write fails. A File whose Read blocks holds Transcribe
// until the Read returns.
//
// A File read error is returned wrapped, ahead of whatever the transport made of the upload it
// cut short. A transport failure or a cancelled ctx is returned wrapped too. A non-2xx reply is
// an *APIError with the OpenAI-style error body's details.
//
// Transcribe is never retried, whatever Config.Retry says: File is a one-shot reader, uploaded
// as it is read, so a second attempt would have no clip to send. A caller who can read the clip
// again retries by calling Transcribe again with a new reader.
func (c *Client) Transcribe(ctx context.Context, req *TranscriptionRequest) (*Transcription, error) {
	if req == nil {
		return nil, errors.New("client: transcribe: no request")
	}
	fields, err := transcriptionFields(req)
	if err != nil {
		return nil, fmt.Errorf("client: transcribe: %w", err)
	}
	file := &clipReader{r: req.File}
	pr, pw := io.Pipe()
	form := multipart.NewWriter(pw)
	uploaded := make(chan struct{})
	go func() {
		defer close(uploaded)
		// The writer's error, or EOF on nil, ends the request body's read.
		pw.CloseWithError(writeTranscriptionForm(form, fields, req.FileName, file))
	}()
	// One attempt, never retried: the clip is read as it is sent, so there is no second copy
	// of it to send again.
	resp, _, err := c.attempt(ctx, transcriptionsRoute, nil, pr, form.FormDataContentType(), "application/json",
		format.DecodeOpenAIError)
	var reply []byte
	if err == nil {
		reply, err = readReply(resp)
	}
	// A request that ended before reading the whole form leaves the goroutine blocked on the
	// pipe; closing the read side fails its write, and it ends. After a whole read, this is a
	// no-op.
	_ = pr.Close()
	<-uploaded
	if file.err != nil {
		// The upload failed on its own, reading File: that is the cause, whatever the
		// transport made of the body's failed read. Any other failure of the goroutine's is a
		// write into the pipe the request closed, which err accounts for.
		return nil, fmt.Errorf("client: transcribe: upload: %w", file.err)
	}
	if err != nil {
		return nil, fmt.Errorf("client: transcribe: %w", err)
	}
	t, err := decodeTranscription(reply)
	if err != nil {
		return nil, fmt.Errorf("client: transcribe: %w", err)
	}
	return t, nil
}

// transcriptionFields validates req and returns the form fields it sends ahead of the file, in
// order: the typed fields, then Native's. The file part's name, file, is always set, so a Native
// key of that name clashes too.
func transcriptionFields(req *TranscriptionRequest) ([][2]string, error) {
	switch {
	case req.Model == "":
		return nil, errors.New("request has no model")
	case req.File == nil:
		return nil, errors.New("request has no file")
	case req.FileName == "":
		return nil, errors.New("request has no file name")
	}
	fields := [][2]string{{"model", req.Model}, {"response_format", "json"}}
	if req.Language != "" {
		fields = append(fields, [2]string{"language", req.Language})
	}
	if req.Prompt != "" {
		fields = append(fields, [2]string{"prompt", req.Prompt})
	}
	set := map[string]bool{"file": true}
	for _, f := range fields {
		set[f[0]] = true
	}
	// Sorted keys make the form the same for the same request, whatever the map's order.
	for _, k := range slices.Sorted(maps.Keys(req.Native)) {
		switch {
		case k == "":
			return nil, errors.New("request's native field has an empty name")
		case set[k]:
			return nil, fmt.Errorf("native field %s clashes with a field the request sets", k)
		}
		for _, v := range req.Native[k] {
			fields = append(fields, [2]string{k, v})
		}
	}
	return fields, nil
}

// writeTranscriptionForm writes fields, then file as the file part named fileName, as the
// transcription endpoint's multipart form, and closes the form.
func writeTranscriptionForm(form *multipart.Writer, fields [][2]string, fileName string, file io.Reader) error {
	for _, f := range fields {
		if err := form.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	// The part's Content-Type is application/octet-stream: a type guessed from the file name
	// would depend on the host's MIME tables, and llama.cpp reads the clip's format from its
	// bytes.
	part, err := form.CreateFormFile("file", fileName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	return form.Close()
}

// clipReader is a TranscriptionRequest's File as the upload reads it, keeping the error a Read
// returned, other than io.EOF, so Transcribe can tell the clip's failure from the request's.
type clipReader struct {
	r   io.Reader
	err error
}

func (c *clipReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if err != nil && err != io.EOF {
		c.err = err
	}
	return n, err
}

// decodeTranscription decodes the body of a 2xx transcription reply.
func decodeTranscription(body []byte) (*Transcription, error) {
	var wire transcriptionReply
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &Transcription{
		Text: wire.Text,
		Usage: TranscriptionUsage{
			InputTokens:  wire.Usage.InputTokens,
			OutputTokens: wire.Usage.OutputTokens,
			TotalTokens:  wire.Usage.TotalTokens,
		},
	}, nil
}

// transcriptionReply is the OpenAI-compatible transcription reply body under response_format
// json. llama.cpp b11669 also sends type transcript.text.done, a usage type of tokens, and
// usage.input_tokens_details.cached_tokens, which the client doesn't keep.
type transcriptionReply struct {
	Text  string `json:"text"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}
