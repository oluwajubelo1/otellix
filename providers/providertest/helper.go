// Package providertest provides utilities for testing LLM providers by mocking HTTP responses.
package providertest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// MockRoundTripper is a simple implementation of http.RoundTripper for testing.
type MockRoundTripper struct {
	Response *http.Response
	Err      error
}

func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.Response, m.Err
}

// NewMockClient returns an *http.Client that uses the MockRoundTripper.
func NewMockClient(resp *http.Response, err error) *http.Client {
	return &http.Client{
		Transport: &MockRoundTripper{Response: resp, Err: err},
	}
}

// NewJSONResponse returns a successful *http.Response with a JSON body.
func NewJSONResponse(statusCode int, body interface{}) *http.Response {
	data, _ := json.Marshal(body)
	resp := &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp
}

// NewStreamResponse returns a successful *http.Response with a streaming body.
func NewStreamResponse(statusCode int, body io.Reader) *http.Response {
	resp := &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(body),
	}
	resp.Header.Set("Content-Type", "text/event-stream")
	return resp
}
