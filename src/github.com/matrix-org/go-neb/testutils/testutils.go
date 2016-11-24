package testutils

import (
	"net/http"
)

// MockTransport implements RoundTripper
type MockTransport struct {
	// RT is the RoundTrip function. Replace this function with your test function.
	// For example:
	//   t := MockTransport{}
	//   t.RT = func(req *http.Request) (*http.Response, error) {
	//       // assert req args, return res or error
	//   }
	RT func(*http.Request) (*http.Response, error)
}

// RoundTrip is a RoundTripper
func (t MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.RT(req)
}

// NewRoundTripper returns a new RoundTripper which will call the provided function.
func NewRoundTripper(roundTrip func(*http.Request) (*http.Response, error)) http.RoundTripper {
	rt := MockTransport{}
	rt.RT = roundTrip
	return rt
}
