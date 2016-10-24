package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProtect(t *testing.T) {
	mockWriter := httptest.NewRecorder()
	mockReq, _ := http.NewRequest("GET", "http://example.com/foo", nil)
	h := Protect(func(w http.ResponseWriter, req *http.Request) {
		var array []string
		w.Write([]byte(array[5])) // NPE
	})

	h(mockWriter, mockReq)

	expectCode := 500
	if mockWriter.Code != expectCode {
		t.Errorf("TestProtect wanted HTTP status %d, got %d", expectCode, mockWriter.Code)
	}

	expectBody := `{"message":"Internal Server Error"}`
	actualBody := mockWriter.Body.String()
	if actualBody != expectBody {
		t.Errorf("TestProtect wanted body %s, got %s", expectBody, actualBody)
	}
}
