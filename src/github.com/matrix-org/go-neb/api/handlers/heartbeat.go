// Package handlers contains the HTTP handlers for Go-NEB.
//
// This includes detail on the API paths and top-level JSON keys. For specific service JSON,
// see the service you're interested in.
//
// See also
//
// Package "api" for the format of the JSON request bodies.
package handlers

import (
	"net/http"

	"github.com/matrix-org/util"
)

// Heartbeat implements the heartbeat API
type Heartbeat struct{}

// OnIncomingRequest returns an empty JSON object which can be used to detect liveness of Go-NEB.
//
// Request:
//  GET /test
//
//
// Response:
//  HTTP/1.1 200 OK
//  {}
func (*Heartbeat) OnIncomingRequest(req *http.Request) util.JSONResponse {
	return util.JSONResponse{
		Code: 200,
		JSON: struct{}{},
	}
}
