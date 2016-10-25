package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

var mux = http.NewServeMux()

func TestMain(m *testing.M) {
	setup(envVars{
		BaseURL:      "http://go.neb",
		DatabaseType: "sqlite3",
		DatabaseURL:  ":memory:",
	}, mux)
	exitCode := m.Run()
	os.Exit(exitCode)
}

func TestNotFound(t *testing.T) {
	mockWriter := httptest.NewRecorder()
	mockReq, _ := http.NewRequest("GET", "http://go.neb/foo", nil)
	mux.ServeHTTP(mockWriter, mockReq)

	expectCode := 404
	if mockWriter.Code != expectCode {
		t.Errorf("TestNotFound wanted HTTP status %d, got %d", expectCode, mockWriter.Code)
	}
}
