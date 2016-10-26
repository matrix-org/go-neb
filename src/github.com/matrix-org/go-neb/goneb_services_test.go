package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

var mux = http.NewServeMux()
var mxTripper = newMatrixTripper()

func TestMain(m *testing.M) {
	setup(envVars{
		BaseURL:      "http://go.neb",
		DatabaseType: "sqlite3",
		DatabaseURL:  ":memory:",
	}, mux, &http.Client{
		Transport: mxTripper,
	})
	exitCode := m.Run()
	os.Exit(exitCode)
}

func TestConfigureClient(t *testing.T) {
	mxTripper.ClearHandlers()
	mockWriter := httptest.NewRecorder()
	syncChan := make(chan string)
	mxTripper.HandlePOSTFilter("@link:hyrule")
	mxTripper.Handle("GET", "/_matrix/client/r0/sync",
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
