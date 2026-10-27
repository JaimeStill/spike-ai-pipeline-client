package client

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"

	"github.com/JaimeStill/spike-ai-pipeline-client/format"
	"github.com/JaimeStill/spike-ai-pipeline-client/internal/native"
)

// embeddingsRoute is the OpenAI-compatible embeddings endpoint, relative to the base URL.
const embeddingsRoute = "embeddings"

// EmbeddingRequest asks an embedding model for one vector per input. Embeddings sit outside the
// protocol and the Format: they go to the OpenAI-compatible embeddings endpoint whatever format
// the Client speaks.
type EmbeddingRequest struct {
	// Model is the embedding model's id. It is required.
	Model string
	// Input is the texts to embed, at least one. An empty string is sent as it is: llama.cpp
	// b11669 embeds it, and an endpoint that refuses it answers with an *APIError.
	Input []string
	// Native is the native tier: a JSON object whose members are merged into the top level of
	// the wire body as they are, for a feature of the endpoint the typed fields don't cover,
	// such as dimensions. Its members mean what the endpoint makes them mean. A member that
	// clashes with one the typed fields set (model, input, encoding_format) is refused.
	Native jsontext.Value
}

// Embeddings is an embedding model's reply: one vector per input, in input order.
type Embeddings struct {
	// Vectors holds the embedding of each input, Vectors[i] that of Input[i].
	Vectors [][]float32
	// Usage counts the tokens the request took.
	Usage EmbeddingUsage
}

// EmbeddingUsage counts the tokens an embedding request took, in the endpoint's names.
// PromptTokens is what protocol.Usage calls InputTokens; an embedding generates no output, so
// there is no output count, and TotalTokens is PromptTokens on endpoints that count nothing else.
type EmbeddingUsage struct {
	// PromptTokens is the tokens of every input together.
	PromptTokens int
	// TotalTokens is the tokens the request took in all.
	TotalTokens int
}

// Embed sends req to the endpoint's embeddings route and returns one vector per input, in input
// order. It refuses a nil request, one with no model or no input, and a Native that isn't a JSON
// object with unique member names or that clashes, before the token source is called or anything
// is sent. The vectors travel as base64 of little-endian float32 (encoding_format "base64"), and
// each reply item's index places its vector; a reply whose items don't match the inputs one to
// one, or whose vector isn't whole float32s, is an error. A non-2xx reply is an *APIError with
// the OpenAI-style error body's details; a transport failure or a cancelled ctx is returned
// wrapped. With a Config.Retry policy, a reply or failure the policy retries is sent again, and
// the last attempt's error is returned as it would be without one.
func (c *Client) Embed(ctx context.Context, req *EmbeddingRequest) (*Embeddings, error) {
	if req == nil {
		return nil, errors.New("client: embed: no request")
	}
	body, err := encodeEmbeddingRequest(req)
	if err != nil {
		return nil, fmt.Errorf("client: embed: %w", err)
	}
	reply, err := c.post(ctx, embeddingsRoute, nil, body, format.DecodeOpenAIError)
	if err != nil {
		return nil, fmt.Errorf("client: embed: %w", err)
	}
	e, err := decodeEmbeddings(reply, len(req.Input))
	if err != nil {
		return nil, fmt.Errorf("client: embed: %w", err)
	}
	return e, nil
}

// encodeEmbeddingRequest validates req and encodes it as the embeddings request body.
func encodeEmbeddingRequest(req *EmbeddingRequest) ([]byte, error) {
	switch {
	case req.Model == "":
		return nil, errors.New("request has no model")
	case len(req.Input) == 0:
		return nil, errors.New("request has no input")
	case !native.Valid(req.Native):
		return nil, errors.New("request's native field isn't a JSON object with unique member names")
	}
	return native.Marshal(embeddingRequest{
		Model:          req.Model,
		Input:          req.Input,
		EncodingFormat: "base64",
		Native:         req.Native,
	})
}

// decodeEmbeddings decodes the body of a 2xx embeddings reply to a request of n inputs, placing
// each vector by its item's index.
func decodeEmbeddings(body []byte, n int) (*Embeddings, error) {
	var wire embeddingReply
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(wire.Data) != n {
		return nil, fmt.Errorf("response has %d embeddings for %d inputs", len(wire.Data), n)
	}
	vectors := make([][]float32, n)
	for i, d := range wire.Data {
		switch {
		case d.Index < 0 || d.Index >= n:
			return nil, fmt.Errorf("response embedding %d has index %d, outside the %d inputs", i, d.Index, n)
		case vectors[d.Index] != nil:
			return nil, fmt.Errorf("response embedding %d repeats index %d", i, d.Index)
		}
		v, err := decodeVector(d.Embedding)
		if err != nil {
			return nil, fmt.Errorf("response embedding %d: %w", i, err)
		}
		vectors[d.Index] = v
	}
	return &Embeddings{
		Vectors: vectors,
		Usage:   EmbeddingUsage{PromptTokens: wire.Usage.PromptTokens, TotalTokens: wire.Usage.TotalTokens},
	}, nil
}

// decodeVector decodes an embedding sent as standard base64 of little-endian float32s. The
// vector is never nil, even when empty, so decodeEmbeddings can tell a placed index from an open
// one.
func decodeVector(s string) ([]float32, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("embedding isn't base64: %w", err)
	}
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("embedding of %d bytes isn't whole float32s", len(b))
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v, nil
}

// embeddingRequest is the OpenAI-compatible embeddings request body.
type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
	// EncodingFormat is always "base64": about a quarter of the bytes of the float array, and exact.
	EncodingFormat string `json:"encoding_format"`
	// Native is the request's native tier, its members merged into the top level. A member
	// named like one the fields above set is a duplicate name, which encoding refuses.
	Native jsontext.Value `json:",embed"`
}

// embeddingReply is the OpenAI-compatible embeddings reply body, as encoding_format base64
// shapes it: each item's embedding a base64 string.
type embeddingReply struct {
	Data []struct {
		Index     int    `json:"index"`
		Embedding string `json:"embedding"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}
