package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

var mux = http.NewServeMux()

type MockTripper struct {
	handlers map[string]func(req *http.Request) (*http.Response, error)
}

func (rt MockTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.Method + " " + req.URL.Path
	h := rt.handlers[key]
	if h == nil {
		panic(
			fmt.Sprintf("Test RoundTrip: Unhandled request: %s\nHandlers: %d",
				key, len(rt.handlers)),
		)
	}
	return h(req)
}

func (rt MockTripper) Handle(method, path string, handler func(req *http.Request) (*http.Response, error)) {
	key := method + " " + path
	if _, exists := rt.handlers[key]; exists {
		panic("Test handler with key " + key + " already exists")
	}
	rt.handlers[key] = handler
}

var tripper = MockTripper{make(map[string]func(req *http.Request) (*http.Response, error))}

type nopCloser struct {
	*bytes.Buffer
}

func (nopCloser) Close() error { return nil }

func newResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       nopCloser{bytes.NewBufferString(body)},
	}
}

func TestMain(m *testing.M) {
	setup(envVars{
		BaseURL:      "http://go.neb",
		DatabaseType: "sqlite3",
		DatabaseURL:  ":memory:",
	}, mux, &http.Client{
		Transport: tripper,
	})
	exitCode := m.Run()
	os.Exit(exitCode)
}

func TestConfigureClient(t *testing.T) {
	for k := range tripper.handlers {
		delete(tripper.handlers, k)
	}
	mockWriter := httptest.NewRecorder()
	tripper.Handle("POST", "/_matrix/client/r0/user/@link:hyrule/filter",
		func(req *http.Request) (*http.Response, error) {
			return newResponse(200, `{
				"filter_id":"abcdef"
			}`), nil
		},
	)
	syncChan := make(chan string)
	tripper.Handle("GET", "/_matrix/client/r0/sync",
		func(req *http.Request) (*http.Response, error) {
			syncChan <- "sync"
			return newResponse(200, `{
				"next_batch":"11_22_33_44",
				"rooms": {}
			}`), nil
		},
	)

	mockReq, _ := http.NewRequest("POST", "http://go.neb/admin/configureClient", bytes.NewBufferString(`
	{
		"UserID":"@link:hyrule",
		"HomeserverURL":"http://hyrule.loz",
		"AccessToken":"dangeroustogoalone",
		"Sync":true,
		"AutoJoinRooms":true
	}`))
	mux.ServeHTTP(mockWriter, mockReq)
	expectCode := 200
	if mockWriter.Code != expectCode {
		t.Errorf("TestConfigureClient wanted HTTP status %d, got %d", expectCode, mockWriter.Code)
	}

	<-syncChan
}
