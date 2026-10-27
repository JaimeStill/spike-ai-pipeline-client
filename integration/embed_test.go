//go:build integration

package integration_test

import (
	"math"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
)

// embeddingModel is set A's embedding model, whose vectors have 768 dimensions.
const embeddingModel = "ggml-org/embeddinggemma-2-GGUF:Q8_0"

// TestEmbed asks for two unrelated inputs' embeddings, then for the second's alone, and checks
// the decoding and the order:
//
//   - each vector has 768 dimensions and unit length, as llama.cpp normalizes embeddings by
//     default; a vector decoded with the wrong byte order or width has neither;
//   - the second input embedded alone matches the pair's second vector, and not its first,
//     which fails if the vectors came back in arrival order rather than input order, or
//     swapped.
func TestEmbed(t *testing.T) {
	c := chatCompletions(t)
	inputs := []string{
		"The cat sat on the warm windowsill all afternoon.",
		"Quarterly revenue rose eight percent on strong cloud sales.",
	}
	pair, err := c.Embed(requestContext(t), &client.EmbeddingRequest{Model: embeddingModel, Input: inputs})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(pair.Vectors) != 2 {
		t.Fatalf("Embed gave %d vectors for 2 inputs", len(pair.Vectors))
	}
	for i, v := range pair.Vectors {
		if len(v) != 768 {
			t.Errorf("vector %d has %d dimensions, want 768", i, len(v))
		}
		if n := norm(v); math.Abs(n-1) > 1e-3 {
			t.Errorf("vector %d has length %g, want 1", i, n)
		}
	}
	if u := pair.Usage; u.PromptTokens == 0 || u.TotalTokens < u.PromptTokens {
		t.Errorf("Usage = %+v, want prompt tokens counted, and total at least them", u)
	}
	alone, err := c.Embed(requestContext(t), &client.EmbeddingRequest{Model: embeddingModel, Input: inputs[1:]})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	same, other := cosine(alone.Vectors[0], pair.Vectors[1]), cosine(alone.Vectors[0], pair.Vectors[0])
	if same < 0.999 || other > 0.9 {
		t.Errorf("the second input alone has cosine %g with the pair's second vector and %g with its first; "+
			"want at least 0.999 and at most 0.9", same, other)
	}
	t.Logf("cosine with its own vector %g, with the other input's %g; usage %+v", same, other, pair.Usage)
}

// norm is v's Euclidean length.
func norm(v []float32) float64 {
	return math.Sqrt(dot(v, v))
}

// cosine is the cosine similarity of a and b, which have the same length.
func cosine(a, b []float32) float64 {
	return dot(a, b) / (norm(a) * norm(b))
}

// dot is the dot product of a and b, which have the same length.
func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}
