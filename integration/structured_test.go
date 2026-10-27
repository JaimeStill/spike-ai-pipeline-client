//go:build integration

package integration_test

import (
	"encoding/json/v2"
	"testing"

	client "github.com/JaimeStill/spike-ai-pipeline-client"
	"github.com/JaimeStill/spike-ai-pipeline-client/protocol"
)

// TestStructuredOutput asks for a country's capital under a strict JSON schema, and wants a
// completed answer whose output_text decodes into a Go value of the schema's shape, as a caller
// decodes it. The router drops Messages' output_config, so through Messages the answer's shape
// is recorded, not asserted.
func TestStructuredOutput(t *testing.T) {
	eachFormatOf(t, func(t *testing.T, c *client.Client, f testFormat) {
		resp, err := c.Generate(requestContext(t), &protocol.Request{
			Model: chatModel,
			Input: []protocol.Item{userText("What is the capital of France?")},
			Text: protocol.TextParam{Format: protocol.JSONSchemaFormat{
				Name:        "capital",
				Description: "A country and its capital city.",
				Schema: []byte(`{
					"type": "object",
					"properties": {
						"capital": {"type": "string"},
						"country": {"type": "string"}
					},
					"required": ["capital", "country"],
					"additionalProperties": false
				}`),
				Strict: true,
			}},
			MaxOutputTokens: chatBound,
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		text := completedText(t, resp)
		t.Logf("output_text %s", text)
		var got struct {
			Capital string `json:"capital"`
			Country string `json:"country"`
		}
		err = json.Unmarshal([]byte(text), &got, json.RejectUnknownMembers(true))
		f.hinted(t, err == nil, "output_text %q doesn't decode as the schema's object: %v", text, err)
		if err == nil {
			f.hinted(t, got.Capital == "Paris" && got.Country == "France", "decoded %+v, want capital Paris, country France", got)
		}
	})
}
