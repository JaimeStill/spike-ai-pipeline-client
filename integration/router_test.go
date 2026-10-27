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
	"github.com/JaimeStill/spike-ai-pipeline-client/format/messages"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// Set A's roles: the model each test sends to.
const (
	chatModel   = "ggml-org/gpt-oss-120b-GGUF:MXFP4"
	visionModel = "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0"
	audioModel  = "ggml-org/gemma-4-E4B-it-GGUF:Q8_0"
)

// chatBound is the MaxOutputTokens of every request to chatModel: gpt-oss-120b reasons ahead of
// its answer or its call, within the same bound.
const chatBound = 1024

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

// testFormat is a format a scenario runs through: its name, its constructor, and whether the
// router drops the hints a scenario records rather than asserts for it.
type testFormat struct {
	name string
	new  func() format.Format
	// dropsHints is set when the router drops the format's structured output, named and none
	// tool choices, and reasoning effort before they reach the model, as it drops Messages'
	// output_config and treats its named and none tool choices as any and none at all.
	dropsHints bool
}

// formats are the formats a scenario runs through with eachFormat, by name: the router serves
// both, each at its own route.
var formats = []testFormat{
	{name: "completions", new: func() format.Format { return completions.New() }},
	{name: "messages", new: func() format.Format { return messages.New() }, dropsHints: true},
}

// eachFormat runs scenario as a subtest of t for each of formats, named for the format, with a
// Client of the router speaking it, so one piece of test code covers every format, the format the
// only difference. Messages requires max_tokens, so a scenario sets MaxOutputTokens on every
// request.
func eachFormat(t *testing.T, scenario func(t *testing.T, c *client.Client)) {
	t.Helper()
	eachFormatOf(t, func(t *testing.T, c *client.Client, _ testFormat) { scenario(t, c) })
}

// eachFormatOf runs scenario as eachFormat does, giving it the format too, for a scenario that
// asserts a hint where the router keeps it and records it where the router drops it.
func eachFormatOf(t *testing.T, scenario func(t *testing.T, c *client.Client, f testFormat)) {
	t.Helper()
	for _, f := range formats {
		t.Run(f.name, func(t *testing.T) {
			scenario(t, newClient(t, f.new()), f)
		})
	}
}

// hinted checks a hint f's request carried. Where the router keeps f's hints, it fails t unless
// ok, with the check wanted and detail. Where it drops them, it records the check's outcome,
// held or not, with detail, and the scenario passes either way.
func (f testFormat) hinted(t *testing.T, ok bool, check, detail string, args ...any) {
	t.Helper()
	switch {
	case f.dropsHints:
		outcome := "held"
		if !ok {
			outcome = "did not hold"
		}
		f.record(t, check+": "+outcome+"; "+detail, args...)
	case !ok:
		t.Errorf("want "+check+"; "+detail, args...)
	}
}

// record logs an outcome a scenario records rather than asserts, as one line a verbose run's log
// carries and a grep for "recorded:" finds: the format, the scenario, t's name without the
// format's subtest, and the outcome.
func (f testFormat) record(t *testing.T, msg string, args ...any) {
	t.Helper()
	scenario := strings.TrimSuffix(t.Name(), "/"+f.name)
	t.Logf("recorded: %s %s: "+msg, append([]any{f.name, scenario}, args...)...)
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
// output is one completed assistant message with non-empty output_text, after the reasoning a
// reasoning model puts ahead of it.
func completedText(t *testing.T, resp *protocol.Response) string {
	t.Helper()
	if resp.Status != protocol.StatusCompleted {
		t.Fatalf("Status = %q (%+v), want completed", resp.Status, resp.IncompleteDetails)
	}
	output := resp.Output
	for len(output) > 0 {
		if _, ok := output[0].(protocol.Reasoning); !ok {
			break
		}
		output = output[1:]
	}
	if len(output) != 1 {
		t.Fatalf("Output has %d items after its reasoning, want 1 message: %#v", len(output), resp.Output)
	}
	m, ok := output[0].(protocol.Message)
	if !ok || m.Role != protocol.RoleAssistant || m.Status != protocol.StatusCompleted {
		t.Fatalf("Output's message = %#v, want a completed assistant message", output[0])
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
