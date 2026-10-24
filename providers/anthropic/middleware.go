package anthropic

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/internal/sdkmw"
	"github.com/oluwajubelo1/otellix/providers"
)

// Middleware returns Anthropic SDK middleware that traces every Messages
// API call the client makes, streaming or not, and applies any budget in
// opts, with no change to the code making the calls:
//
//	client := anthropic.NewClient(
//	    option.WithMiddleware(otellixanthropic.Middleware(otellix.WithBudgetConfig(budget))),
//	)
//
// opts take the same options as otellix.Trace. User and project IDs are
// read from the request's context, as set by otellix.ContextWithUser or the
// web middleware. A call blocked by a budget returns its
// otellix.BudgetExceededError from the SDK, without being sent or retried.
// Other requests, such as token counting, pass through untouched.
func Middleware(opts ...otellix.Option) option.Middleware {
	return sdkmw.New("anthropic", []sdkmw.API{messagesAPI}, opts)
}

var messagesAPI = sdkmw.API{
	Match: func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/messages") },

	ParseRequest: func(body []byte) (providers.CallParams, bool, error) {
		var req struct {
			Model    string          `json:"model"`
			System   json.RawMessage `json:"system"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			MaxTokens int  `json:"max_tokens"`
			Stream    bool `json:"stream"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return providers.CallParams{}, false, err
		}
		params := providers.CallParams{Model: req.Model, SystemPrompt: sdkmw.Text(req.System), MaxTokens: req.MaxTokens}
		for _, m := range req.Messages {
			params.Messages = append(params.Messages, providers.Message{Role: m.Role, Content: sdkmw.Text(m.Content)})
		}
		return params, req.Stream, nil
	},

	ParseResponse: func(body []byte) (providers.CallResult, error) {
		var resp struct {
			Model string `json:"model"`
			Usage usage  `json:"usage"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return providers.CallResult{}, err
		}
		return providers.CallResult{
			InputTokens:      resp.Usage.InputTokens,
			OutputTokens:     resp.Usage.OutputTokens,
			CacheReadTokens:  resp.Usage.CacheReadInputTokens,
			CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
			Model:            resp.Model,
		}, nil
	},

	ParseEvent: func(_ string, data []byte) (providers.StreamEvent, bool) {
		var evt struct {
			Type    string `json:"type"`
			Message struct {
				Usage usage `json:"usage"`
			} `json:"message"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
			Usage usage `json:"usage"`
		}
		if json.Unmarshal(data, &evt) != nil {
			return providers.StreamEvent{}, false
		}
		switch evt.Type {
		case "message_start":
			u := evt.Message.Usage
			return providers.StreamEvent{
				InputTokens:      u.InputTokens,
				CacheReadTokens:  u.CacheReadInputTokens,
				CacheWriteTokens: u.CacheCreationInputTokens,
			}, true
		case "content_block_delta":
			return providers.StreamEvent{Token: evt.Delta.Text}, evt.Delta.Text != ""
		case "message_delta":
			// output_tokens here is the total for the whole message.
			return providers.StreamEvent{OutputTokens: evt.Usage.OutputTokens}, evt.Usage.OutputTokens > 0
		}
		return providers.StreamEvent{}, false
	},
}

// usage is a Messages API usage object.
type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}
