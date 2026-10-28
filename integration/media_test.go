//go:build integration

package integration_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

func TestImageDescription(t *testing.T) {
	eachFormat(t, func(t *testing.T, c *client.Client) {
		resp, err := c.Generate(requestContext(t), &protocol.Request{
			Model: visionModel,
			Input: []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				protocol.InputText{Text: "Describe this image in one sentence."},
				protocol.InputImage{ImageURL: protocol.DataURL("image/png", readTestdata(t, "shapes.png"))},
			}}},
			MaxOutputTokens: 512,
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		text := completedText(t, resp)
		t.Logf("%s: %q", visionModel, text)
		// shapes.png is a red circle, a blue square, and a green triangle. Each shape must be
		// named as a word of its own, singular or plural, in any phrasing, as the root's probe
		// asserts.
		words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
		for _, want := range []string{"circle", "square", "triangle"} {
			if !slices.Contains(words, want) && !slices.Contains(words, want+"s") {
				t.Errorf("the description %q doesn't name a %s", text, want)
			}
		}
	})
}

func TestAudioInChat(t *testing.T) {
	c := chatCompletions(t)
	resp, err := c.Generate(requestContext(t), &protocol.Request{
		Model: audioModel,
		Input: []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
			protocol.InputText{Text: "Transcribe this audio clip."},
			protocol.InputAudio{Data: readTestdata(t, "phrase.wav"), Format: protocol.AudioFormatWAV},
		}}},
		MaxOutputTokens: 512,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	text := completedText(t, resp)
	t.Logf("%s: %q", audioModel, text)
	assertAccessCode(t, text)
}

// TestAudioInResponses sends audio as the client extension, spike-ai-pipeline:input_audio, which
// the router's Open Responses route doesn't take, and records its answer: a transcript, or the
// error it refuses the part with. It passes either way.
func TestAudioInResponses(t *testing.T) {
	f := openResponses
	c := newClient(t, f.new())
	resp, err := c.Generate(requestContext(t), &protocol.Request{
		Model: audioModel,
		Input: []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
			protocol.InputText{Text: "Transcribe this audio clip."},
			protocol.InputAudio{Data: readTestdata(t, "phrase.wav"), Format: protocol.AudioFormatWAV},
		}}},
		MaxOutputTokens: 512,
	})
	var apiErr *client.APIError
	switch {
	case errors.As(err, &apiErr):
		f.record(t, "refused: %v", apiErr)
	case err != nil:
		t.Fatalf("Generate: %v", err)
	default:
		f.record(t, "answered: status %q, output %#v", resp.Status, resp.Output)
	}
}

// assertAccessCode fails t unless text, a transcript of phrase.wav, carries its code.
// phrase.wav says "The access code is seven four two nine." The code may come back as digits or
// words, spaced or not: compared with everything but letters and digits stripped, either form
// passes.
func assertAccessCode(t *testing.T, text string) {
	t.Helper()
	got := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, text)
	if !strings.Contains(got, "7429") && !strings.Contains(got, "sevenfourtwonine") {
		t.Errorf("the transcript %q doesn't carry the code 7429", text)
	}
}
