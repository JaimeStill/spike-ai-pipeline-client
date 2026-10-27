//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
)

// TestTranscribe streams phrase.wav from its file to the transcription endpoint, the clip read
// as it uploads.
func TestTranscribe(t *testing.T) {
	c := chatCompletions(t)
	f, err := os.Open(filepath.Join("testdata", "phrase.wav"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	got, err := c.Transcribe(requestContext(t), &client.TranscriptionRequest{
		Model:    audioModel,
		File:     f,
		FileName: "phrase.wav",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	t.Logf("%s: %q; usage %+v", audioModel, got.Text, got.Usage)
	assertAccessCode(t, got.Text)
	if u := got.Usage; u.InputTokens == 0 || u.OutputTokens == 0 || u.TotalTokens < u.InputTokens+u.OutputTokens {
		t.Errorf("Usage = %+v, want input and output tokens counted, and total at least both", u)
	}
}
