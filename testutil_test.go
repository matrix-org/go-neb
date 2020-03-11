package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
)

// newResponse creates a new HTTP response with the given data.
func newResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       ioutil.NopCloser(bytes.NewBufferString(body)),
	}
}

// matrixTripper mocks out RoundTrip and calls a registered handler instead.
type matrixTripper struct {
	handlers map[string]func(req *http.Request) (*http.Response, error)
}

func newMatrixTripper() *matrixTripper {
	return &matrixTripper{
		handlers: make(map[string]func(req *http.Request) (*http.Response, error)),
	}
}

func (rt *matrixTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.Method + " " + req.URL.Path
	h := rt.handlers[key]
	if h == nil {
		panic(fmt.Sprintf(
			"RoundTrip: Unhandled request: %s\nHandlers: %d",
			key, len(rt.handlers),
		))
	}
	return h(req)
}

func (rt *matrixTripper) Handle(method, path string, handler func(req *http.Request) (*http.Response, error)) {
	key := method + " " + path
	if _, exists := rt.handlers[key]; exists {
		panic(fmt.Sprintf("Test handler with key %s already exists", key))
	}
	rt.handlers[key] = handler
}

func (rt *matrixTripper) HandlePOSTFilter(userID string) {
	rt.Handle("POST", "/_matrix/client/r0/user/"+userID+"/filter",
		func(req *http.Request) (*http.Response, error) {
			return newResponse(200, `{
				"filter_id":"abcdef"
			}`), nil
		},
	)
}

func (rt *matrixTripper) ClearHandlers() {
	for k := range rt.handlers {
		delete(rt.handlers, k)
	}
}
