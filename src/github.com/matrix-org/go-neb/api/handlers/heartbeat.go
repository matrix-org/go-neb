package handlers

import (
	"github.com/matrix-org/go-neb/errors"
	"net/http"
)

// Heartbeat implements the heartbeat API
type Heartbeat struct{}

// OnIncomingRequest returns an empty JSON object which can be used to detect liveness of Go-NEB.
//
// Request:
// ```
// GET /test
// ```
//
// Response:
// ```
// HTTP/1.1 200 OK
// Content-Type: applicatoin/json
// {}
// ```
func (*Heartbeat) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	return &struct{}{}, nil
}
