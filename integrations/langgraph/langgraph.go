// Package langgraph provides drop-in telemetry for LangGraph-style agent
// applications built on LangChainGo. LangGraph Go executes agent nodes via
// langchaingo callbacks, so the same Otellix handler captures chain/LLM events
// end to end. This package layers agent identity (name + step) on top by tagging
// the context so every downstream LLM call is attributed to the agent and step.
//
// Usage:
//
//	handler := langgraph.NewOtellixHandler(
//	    langgraph.WithProvider("openai"),
//	    langgraph.WithModel("gpt-4o"),
//	)
//	// attach handler to your langchaingo LLM, then in each agent node:
//	ctx = langgraph.ContextWithAgent(ctx, "researcher")
//	ctx = langgraph.ContextWithStep(ctx, nodeNumber)
package langgraph

import (
	"context"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/integrations/langchaingo"
)

// OtellixHandler is the LangChainGo-compatible callback handler. See
// langchaingo.NewOtellixHandler.
type OtellixHandler = langchaingo.OtellixHandler

// NewOtellixHandler creates an Otellix callback handler for LangChainGo/LangGraph.
func NewOtellixHandler(opts ...langchaingo.Option) *OtellixHandler {
	return langchaingo.NewOtellixHandler(opts...)
}

// WithProvider sets the provider name for cost tracking.
func WithProvider(provider string) langchaingo.Option {
	return langchaingo.WithProvider(provider)
}

// WithModel sets the model for cost tracking.
func WithModel(model string) langchaingo.Option {
	return langchaingo.WithModel(model)
}

// ContextWithAgent tags the context with the agent name so all LLM calls made
// within the node are attributed to it.
func ContextWithAgent(ctx context.Context, agent string) context.Context {
	return otellix.ContextWithAgent(ctx, agent)
}

// ContextWithStep tags the context with the agent step number.
func ContextWithStep(ctx context.Context, step int) context.Context {
	return otellix.ContextWithStep(ctx, step)
}
