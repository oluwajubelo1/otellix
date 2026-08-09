package otellix

import (
	"regexp"
	"strings"

	"github.com/oluwajubelo1/otellix/providers"
)

// CaptureConfig controls whether prompt/response content is captured on spans
// and how PII is redacted before it leaves the process.
//
// By default Otellix never captures raw content — only token counts and costs.
// Enable content capture explicitly and, ideally, pair it with redaction.
type CaptureConfig struct {
	// CaptureInput captures the system prompt and user messages on the span as
	// llm.prompt / gen_ai.content.* attributes.
	CaptureInput bool

	// CaptureOutput captures the assistant response text on the span as
	// llm.response / gen_ai.content.assistant.
	CaptureOutput bool

	// RedactPatterns is a list of regex patterns. Matching substrings are
	// replaced with [REDACTED] before content is attached to spans.
	RedactPatterns []string

	// Redactor, if set, is applied to every captured string. It runs after
	// RedactPatterns.
	Redactor func(string) string

	compiled []*regexp.Regexp
}

// WithCaptureInput is a convenience option: enable input capture.
func WithCaptureInput() Option {
	return func(c *Config) {
		if c.CaptureContent == nil {
			c.CaptureContent = &CaptureConfig{}
		}
		c.CaptureContent.CaptureInput = true
	}
}

// WithCaptureOutput is a convenience option: enable output capture.
func WithCaptureOutput() Option {
	return func(c *Config) {
		if c.CaptureContent == nil {
			c.CaptureContent = &CaptureConfig{}
		}
		c.CaptureContent.CaptureOutput = true
	}
}

// WithRedaction adds PII redaction patterns for captured content.
func WithRedaction(patterns ...string) Option {
	return func(c *Config) {
		if c.CaptureContent == nil {
			c.CaptureContent = &CaptureConfig{}
		}
		c.CaptureContent.RedactPatterns = append(c.CaptureContent.RedactPatterns, patterns...)
	}
}

func (cc *CaptureConfig) init() {
	if cc == nil {
		return
	}
	if cc.compiled != nil {
		return
	}
	for _, p := range cc.RedactPatterns {
		if re, err := regexp.Compile(p); err == nil {
			cc.compiled = append(cc.compiled, re)
		}
	}
}

// Redact applies configured patterns and the custom redactor to s.
func (cc *CaptureConfig) Redact(s string) string {
	if cc == nil || s == "" {
		return s
	}
	cc.init()
	for _, re := range cc.compiled {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	if cc.Redactor != nil {
		s = cc.Redactor(s)
	}
	return strings.TrimSpace(s)
}

// promptText assembles the full prompt (system + messages) for content capture.
func promptText(params providers.CallParams) string {
	var sb strings.Builder
	if params.SystemPrompt != "" {
		sb.WriteString("system: ")
		sb.WriteString(params.SystemPrompt)
		sb.WriteString("\n")
	}
	for _, m := range params.Messages {
		if m.Content == "" {
			continue
		}
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// lastUserMessage returns the most recent user message content, if any.
func lastUserMessage(params providers.CallParams) string {
	for i := len(params.Messages) - 1; i >= 0; i-- {
		if params.Messages[i].Role == "user" && params.Messages[i].Content != "" {
			return params.Messages[i].Content
		}
	}
	return ""
}
