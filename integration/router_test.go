//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/format"
	"github.com/JaimeStill/spike-ai-pipeline-client/format/completions"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// Set A's roles: the model each test sends to.
const (
	chatModel   = "ggml-org/gpt-oss-120b-GGUF:MXFP4"
	visionModel = "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0"
	audioModel  = "ggml-org/gemma-4-E4B-it-GGUF:Q8_0"
)

// requestTimeout bounds one request. Set A is already loaded, so this covers generation only,
// which on a reasoning model such as gpt-oss-120b takes tens of seconds.
const requestTimeout = 3 * time.Minute

// newClient returns a Client of the router SPIKE_ROUTER_URL names, speaking f, with
// autoload=false on every request. It skips t when SPIKE_ROUTER_URL is unset.
func newClient(t *testing.T, f format.Format) *client.Client {
	t.Helper()
	base := os.Getenv("SPIKE_ROUTER_URL")
	if base == "" {
		t.Skip("SPIKE_ROUTER_URL is unset: set it to the router to test against, such as http://gb10-2:8080")
	}
	c, err := client.New(client.Config{
		BaseURL: strings.TrimSuffix(base, "/") + "/v1",
		Query:   url.Values{"autoload": {"false"}},
		HTTP:    &http.Client{},
		Format:  f,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// requestContext is t's context, bounded by requestTimeout.
func requestContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	t.Cleanup(cancel)
	return ctx
}

// chatCompletions returns a Client of the router speaking Chat Completions.
func chatCompletions(t *testing.T) *client.Client {
	t.Helper()
	return newClient(t, completions.New())
}

// readTestdata returns the file name in testdata.
func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// completedText returns the output_text of resp, failing t unless resp is completed and its
// output is one completed assistant message with non-empty output_text.
func completedText(t *testing.T, resp *protocol.Response) string {
	t.Helper()
	if resp.Status != protocol.StatusCompleted {
		t.Fatalf("Status = %q (%+v), want completed", resp.Status, resp.IncompleteDetails)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("Output has %d items, want 1 message: %#v", len(resp.Output), resp.Output)
	}
	m, ok := resp.Output[0].(protocol.Message)
	if !ok || m.Role != protocol.RoleAssistant || m.Status != protocol.StatusCompleted {
		t.Fatalf("Output[0] = %#v, want a completed assistant message", resp.Output[0])
	}
	var text strings.Builder
	for _, p := range m.Content {
		if o, ok := p.(protocol.OutputText); ok {
			text.WriteString(o.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		t.Fatalf("the message has no output_text: %#v", m.Content)
	}
	return text.String()
}
