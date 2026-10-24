// Package sdkmw traces LLM calls made with an official provider SDK, from
// inside the SDK's HTTP middleware. It reads the request and response
// bodies, so existing SDK calls get tracing, cost and budgets with no
// change to the code that makes them.
//
// The OpenAI and Anthropic Go SDKs both define middleware as
//
//	func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error)
//
// via type aliases, so one implementation serves both.
package sdkmw

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

// Next sends a request on to the next middleware or the HTTP client.
type Next = func(*http.Request) (*http.Response, error)

// Middleware is the shape of the OpenAI and Anthropic SDK middleware.
type Middleware = func(*http.Request, Next) (*http.Response, error)

// API describes one SDK endpoint: which requests it covers and how to read
// their bodies.
type API struct {
	// Match reports whether a request is a call to this endpoint.
	Match func(r *http.Request) bool

	// ParseRequest reads the call's parameters from the request body.
	ParseRequest func(body []byte) (params providers.CallParams, stream bool, err error)

	// ParseResponse reads token usage from a non-streaming response body.
	ParseResponse func(body []byte) (providers.CallResult, error)

	// ParseEvent reads one server-sent event of a streaming response. ok is
	// false for events with nothing to record. Token and usage fields are
	// as reported by the provider; the caller fills in estimates.
	ParseEvent func(event string, data []byte) (evt providers.StreamEvent, ok bool)
}

// New returns middleware that traces calls to any of apis, for provider.
// Requests that match none of them pass through untouched.
func New(provider string, apis []API, opts []otellix.Option) Middleware {
	// The SDK needs a real HTTP response, which a cached CallResult can't
	// give it, so request caching is turned off.
	opts = append(opts[:len(opts):len(opts)], func(c *otellix.Config) { c.CacheConfig = nil })

	return func(req *http.Request, next Next) (*http.Response, error) {
		if req.Method != http.MethodPost {
			return next(req)
		}
		for _, api := range apis {
			if api.Match(req) {
				return handle(provider, api, opts, req, next)
			}
		}
		return next(req)
	}
}

func handle(provider string, api API, opts []otellix.Option, req *http.Request, next Next) (*http.Response, error) {
	body, err := readBody(req)
	if err != nil {
		return nil, err
	}
	params, stream, err := api.ParseRequest(body)
	if err != nil {
		// Not a body we understand. Let the call through untraced rather
		// than break it.
		return next(req)
	}

	h := &call{provider: provider, api: api, req: req, next: next, model: params.Model}
	if stream {
		return h.stream(params, opts)
	}
	return h.call(params, opts)
}

// call is one SDK request. It implements providers.Provider, so it can be
// passed to otellix.Trace and otellix.TraceStream.
type call struct {
	provider string
	api      API
	req      *http.Request
	next     Next
	model    string // the model in the request body

	resp         *http.Response // the provider's response, once sent
	transportErr error          // set when the request couldn't be sent
	events       *eventSource   // a streaming response's events
}

func (c *call) Name() string { return c.provider }

// send passes the request on, with ctx carrying the call's span. If a
// budget downgrade changed the model, the request body is rewritten to use
// the new one.
func (c *call) send(ctx context.Context, params providers.CallParams) error {
	req := c.req.WithContext(ctx)
	if params.Model != c.model {
		if err := setModel(req, params.Model); err != nil {
			c.transportErr = err
			return err
		}
	}
	resp, err := c.next(req)
	if err != nil {
		c.transportErr = err
		return err
	}
	c.resp = resp
	if resp.StatusCode >= 400 {
		return statusError(c.provider, params.Model, resp)
	}
	return nil
}

func (c *call) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	if err := c.send(ctx, params); err != nil {
		return providers.CallResult{}, err
	}

	data, err := io.ReadAll(c.resp.Body)
	_ = c.resp.Body.Close()
	c.resp.Body = io.NopCloser(bytes.NewReader(data))
	if err != nil {
		c.transportErr = err
		return providers.CallResult{}, err
	}

	// A body without usage is recorded as zero tokens rather than failing
	// a call that succeeded.
	result, _ := c.api.ParseResponse(data)
	return result, nil
}

func (c *call) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	if err := c.send(ctx, params); err != nil {
		return nil, err
	}
	c.events = newEventSource(c.api, params, c.resp.Body)
	return c.events, nil
}

// call traces a non-streaming request.
func (c *call) call(params providers.CallParams, opts []otellix.Option) (*http.Response, error) {
	_, err := otellix.Trace(c.req.Context(), c, params, opts...)
	return c.result(err)
}

// stream traces a streaming request. The response body is wrapped so that
// events are traced as the SDK reads them.
func (c *call) stream(params providers.CallParams, opts []otellix.Option) (*http.Response, error) {
	s, err := otellix.TraceStream(c.req.Context(), c, params, opts...)
	if err != nil {
		return c.result(err)
	}
	c.resp.Body = &streamBody{stream: s, events: c.events}
	return c.resp, nil
}

// result returns what the SDK should get for a call that ended with err.
func (c *call) result(err error) (*http.Response, error) {
	switch {
	case c.transportErr != nil:
		// Let the SDK see the transport error as it would without us, so
		// its retries work as usual.
		return c.resp, c.transportErr
	case c.resp != nil:
		// The provider answered. The SDK turns error statuses into its own
		// error types.
		return c.resp, nil
	case err != nil:
		// Otellix stopped the call before it was sent: a budget or
		// unpriced-model block.
		return blocked(c.req), err
	default:
		return nil, errors.New("otellix: call ended without a response")
	}
}

// blocked is the response returned alongside an error for a call Otellix
// stopped. The SDKs retry a nil response as a connection error, so this
// tells them not to: retrying won't free up any budget.
func blocked(req *http.Request) *http.Response {
	return &http.Response{
		Status:     "403 Forbidden (blocked by otellix)",
		StatusCode: http.StatusForbidden,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{"X-Should-Retry": []string{"false"}},
		Body:       http.NoBody,
		Request:    req,
	}
}

// statusError describes an HTTP error response for the span.
func statusError(provider, model string, resp *http.Response) error {
	err := fmt.Errorf("HTTP %s", resp.Status)
	if resp.StatusCode == http.StatusTooManyRequests {
		return &providers.RateLimitError{Provider: provider, Model: model, RetryAfter: resp.Header.Get("Retry-After"), Err: err}
	}
	return &providers.ProviderError{Provider: provider, Model: model, Err: err}
}

// setModel rewrites the "model" field of a JSON request body, keeping every
// other field as it was.
func setModel(req *http.Request, model string) error {
	body, err := readBody(req)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	if fields["model"], err = json.Marshal(model); err != nil {
		return err
	}
	if body, err = json.Marshal(fields); err != nil {
		return err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	req.ContentLength = int64(len(body))
	return nil
}

// readBody returns the request body, leaving it readable for the next handler.
func readBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	if req.GetBody != nil {
		rc, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(rc)
	}
	data, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(data))
	return data, err
}

// eventSource parses a server-sent event stream into stream events. It
// keeps each event's bytes for the SDK to read, and is the providers.Stream
// behind the otellix.Stream.
type eventSource struct {
	api  API
	body io.ReadCloser
	r    *bufio.Reader

	// pending holds an event's bytes for the SDK, and next the event for
	// the otellix.Stream, once a whole event has been read.
	pending bytes.Buffer
	next    providers.StreamEvent
	eof     bool

	event string       // the current event's name
	data  bytes.Buffer // the current event's data

	inputEstimate int64 // input tokens estimated from the request
	sentInput     bool  // whether an input count has been passed on
	outputChars   int   // characters of output text so far
}

func newEventSource(api API, params providers.CallParams, body io.ReadCloser) *eventSource {
	contents := make([]string, len(params.Messages))
	for i, m := range params.Messages {
		contents[i] = m.Content
	}
	return &eventSource{
		api:           api,
		body:          body,
		r:             bufio.NewReader(body),
		inputEstimate: otellix.EstimateTokens(params.SystemPrompt) + otellix.EstimateTokensFromMessages(contents),
	}
}

// readEvent reads up to the end of the next event the API records, keeping
// the bytes read in pending. It returns io.EOF at the end of the body.
func (e *eventSource) readEvent() (providers.StreamEvent, error) {
	for {
		line, err := e.r.ReadBytes('\n')
		e.pending.Write(line)
		if len(line) > 0 {
			if evt, ok := e.parseLine(bytes.TrimRight(line, "\r\n")); ok {
				return evt, nil
			}
		}
		if err != nil {
			// An event the provider didn't end with a blank line still counts.
			if evt, ok := e.dispatch(); ok {
				return evt, nil
			}
			return providers.StreamEvent{}, err
		}
	}
}

// parseLine handles one line of the stream, and returns an event when a
// blank line ends one.
func (e *eventSource) parseLine(line []byte) (providers.StreamEvent, bool) {
	switch {
	case len(line) == 0:
		return e.dispatch()
	case bytes.HasPrefix(line, []byte("event:")):
		e.event = string(bytes.TrimSpace(line[len("event:"):]))
	case bytes.HasPrefix(line, []byte("data:")):
		if e.data.Len() > 0 {
			e.data.WriteByte('\n')
		}
		e.data.Write(bytes.TrimPrefix(line[len("data:"):], []byte(" ")))
	}
	return providers.StreamEvent{}, false
}

// dispatch parses the event read so far, and fills in estimates for the
// usage the provider hasn't reported yet.
func (e *eventSource) dispatch() (providers.StreamEvent, bool) {
	event, data := e.event, e.data.Bytes()
	e.event = ""
	defer e.data.Reset()
	if len(data) == 0 {
		return providers.StreamEvent{}, false
	}

	evt, ok := e.api.ParseEvent(event, data)
	if !ok {
		return providers.StreamEvent{}, false
	}

	// Estimate input until the provider reports it, so the budget check
	// sees it from the start. Reported usage replaces the estimate.
	if evt.InputTokens > 0 || evt.CacheReadTokens > 0 || evt.CacheWriteTokens > 0 {
		e.sentInput = true
	} else if !e.sentInput {
		evt.InputTokens = e.inputEstimate
		e.sentInput = true
	}
	// Likewise estimate output from the text so far. Providers that report
	// output usage do so at the end of the stream, which replaces it.
	e.outputChars += len(evt.Token)
	if evt.OutputTokens == 0 && evt.Token != "" {
		evt.OutputTokens = max(int64(e.outputChars/4), 1) // ~4 characters per token, as EstimateTokens
	}
	return evt, true
}

// Recv implements providers.Stream. It returns the event read by the last
// readEvent call, then io.EOF once the body is finished.
func (e *eventSource) Recv() (providers.StreamEvent, error) {
	if e.eof {
		return providers.StreamEvent{}, io.EOF
	}
	return e.next, nil
}

// Close implements providers.Stream and io.Closer.
func (e *eventSource) Close() error { return e.body.Close() }

// streamBody is the response body the SDK reads for a traced stream. Each
// event is passed through the otellix.Stream before the SDK sees it, so a
// stream that runs over budget is cut off.
type streamBody struct {
	stream *otellix.Stream
	events *eventSource
	err    error
}

func (b *streamBody) Read(p []byte) (int, error) {
	for b.events.pending.Len() == 0 {
		if b.err != nil {
			return 0, b.err
		}
		b.fill()
	}
	return b.events.pending.Read(p)
}

// fill reads the next event and records it, setting b.err when the stream
// ends or is cut off.
func (b *streamBody) fill() {
	evt, err := b.events.readEvent()
	if err != nil {
		b.events.eof = true
		_, _ = b.stream.Recv() // returns io.EOF
		_ = b.stream.Close()
		b.err = err
		return
	}

	b.events.next = evt
	if _, err := b.stream.Recv(); err != nil {
		// Over budget: the stream is closed, and the event that crossed the
		// limit isn't passed on.
		b.events.pending.Reset()
		b.err = err
	}
}

func (b *streamBody) Close() error {
	return b.stream.Close()
}
