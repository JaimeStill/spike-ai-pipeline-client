//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/chatcompletions"
)

// Set A's roles: the model each test sends to.
const chatModel = "ggml-org/gpt-oss-120b-GGUF:MXFP4"

// requestTimeout bounds one request. Set A is already loaded, so this covers generation only,
// which on a reasoning model such as gpt-oss-120b takes tens of seconds.
const requestTimeout = 3 * time.Minute

// newClient returns a Client of the router SPIKE_ROUTER_URL names, speaking format, with
// autoload=false on every request. It skips t when SPIKE_ROUTER_URL is unset.
func newClient(t *testing.T, format client.Format) *client.Client {
	t.Helper()
	base := os.Getenv("SPIKE_ROUTER_URL")
	if base == "" {
		t.Skip("SPIKE_ROUTER_URL is unset: set it to the router to test against, such as http://gb10-2:8080")
	}
	c, err := client.New(client.Config{
		BaseURL: strings.TrimSuffix(base, "/") + "/v1",
		Query:   url.Values{"autoload": {"false"}},
		HTTP:    &http.Client{},
		Format:  format,
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
	return newClient(t, chatcompletions.New())
}
