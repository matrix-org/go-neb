package testutils

import (
	"net/http"
)

// MockTransport implements RoundTripper
type MockTransport struct {
	RT func(*http.Request) (*http.Response, error)
}

// RoundTrip is a RoundTripper
func (t MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.RT(req)
}
