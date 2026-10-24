package openai

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/openai/openai-go/option"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/internal/sdkmw"
	"github.com/oluwajubelo1/otellix/providers"
)

// Middleware returns OpenAI SDK middleware that traces every Chat
// Completions and Responses API call the client makes, streaming or not,
// and applies any budget in opts, with no change to the code making the
// calls:
//
//	client := openai.NewClient(
//	    option.WithMiddleware(otellixopenai.Middleware(otellix.WithBudgetConfig(budget))),
//	)
//
// opts take the same options as otellix.Trace. User and project IDs are
// read from the request's context, as set by otellix.ContextWithUser or the
// web middleware. A call blocked by a budget returns its
// otellix.BudgetExceededError from the SDK, without being sent or retried.
// Other requests, such as embeddings, pass through untouched.
func Middleware(opts ...otellix.Option) option.Middleware {
	return sdkmw.New("openai", []sdkmw.API{chatCompletionsAPI, responsesAPI}, opts)
}

var chatCompletionsAPI = sdkmw.API{
	Match: func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/chat/completions") },

	ParseRequest: func(body []byte) (providers.CallParams, bool, error) {
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			MaxTokens           int  `json:"max_tokens"`
			MaxCompletionTokens int  `json:"max_completion_tokens"`
			Stream              bool `json:"stream"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return providers.CallParams{}, false, err
		}
		params := providers.CallParams{Model: req.Model, MaxTokens: req.MaxCompletionTokens}
		if params.MaxTokens == 0 {
			params.MaxTokens = req.MaxTokens
		}
		for _, m := range req.Messages {
			params.Messages = append(params.Messages, providers.Message{Role: m.Role, Content: sdkmw.Text(m.Content)})
		}
		return params, req.Stream, nil
	},

	ParseResponse: func(body []byte) (providers.CallResult, error) {
		var resp struct {
			Model string     `json:"model"`
			Usage *chatUsage `json:"usage"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return providers.CallResult{}, err
		}
		result := resp.Usage.result()
		result.Model = resp.Model
		return result, nil
	},

	ParseEvent: func(_ string, data []byte) (providers.StreamEvent, bool) {
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *chatUsage `json:"usage"`
		}
		if json.Unmarshal(data, &chunk) != nil { // e.g. the final "[DONE]"
			return providers.StreamEvent{}, false
		}
		var evt providers.StreamEvent
		if len(chunk.Choices) > 0 {
			evt.Token = chunk.Choices[0].Delta.Content
		}
		// Usage comes in a final chunk, only when the request sets
		// stream_options.include_usage. Without it, usage is estimated.
		if chunk.Usage != nil {
			r := chunk.Usage.result()
			evt.InputTokens, evt.OutputTokens, evt.CacheReadTokens = r.InputTokens, r.OutputTokens, r.CacheReadTokens
		}
		return evt, evt.Token != "" || chunk.Usage != nil
	},
}

// chatUsage is a Chat Completions usage object.
type chatUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// result maps usage to a CallResult. prompt_tokens includes cached tokens,
// which are priced separately, so they are taken out of input.
func (u *chatUsage) result() providers.CallResult {
	if u == nil {
		return providers.CallResult{}
	}
	cached := u.PromptTokensDetails.CachedTokens
	return providers.CallResult{
		InputTokens:     u.PromptTokens - cached,
		OutputTokens:    u.CompletionTokens,
		CacheReadTokens: cached,
	}
}

var responsesAPI = sdkmw.API{
	Match: func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/responses") },

	ParseRequest: func(body []byte) (providers.CallParams, bool, error) {
		var req struct {
			Model           string          `json:"model"`
			Instructions    string          `json:"instructions"`
			Input           json.RawMessage `json:"input"`
			MaxOutputTokens int             `json:"max_output_tokens"`
			Stream          bool            `json:"stream"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return providers.CallParams{}, false, err
		}
		return providers.CallParams{
			Model:        req.Model,
			SystemPrompt: req.Instructions,
			Messages:     []providers.Message{{Role: "user", Content: sdkmw.Text(req.Input)}},
			MaxTokens:    req.MaxOutputTokens,
		}, req.Stream, nil
	},

	ParseResponse: func(body []byte) (providers.CallResult, error) {
		var resp struct {
			Model string          `json:"model"`
			Usage *responsesUsage `json:"usage"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return providers.CallResult{}, err
		}
		result := resp.Usage.result()
		result.Model = resp.Model
		return result, nil
	},

	ParseEvent: func(_ string, data []byte) (providers.StreamEvent, bool) {
		var evt struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response struct {
				Usage *responsesUsage `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal(data, &evt) != nil {
			return providers.StreamEvent{}, false
		}
		switch evt.Type {
		case "response.output_text.delta":
			return providers.StreamEvent{Token: evt.Delta}, evt.Delta != ""
		case "response.completed", "response.incomplete", "response.failed":
			if evt.Response.Usage == nil {
				return providers.StreamEvent{}, false
			}
			r := evt.Response.Usage.result()
			return providers.StreamEvent{InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, CacheReadTokens: r.CacheReadTokens}, true
		}
		return providers.StreamEvent{}, false
	},
}

// responsesUsage is a Responses API usage object.
type responsesUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	InputTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

// result maps usage to a CallResult. input_tokens includes cached tokens,
// which are priced separately, so they are taken out of input.
func (u *responsesUsage) result() providers.CallResult {
	if u == nil {
		return providers.CallResult{}
	}
	cached := u.InputTokensDetails.CachedTokens
	return providers.CallResult{
		InputTokens:     u.InputTokens - cached,
		OutputTokens:    u.OutputTokens,
		CacheReadTokens: cached,
	}
}
