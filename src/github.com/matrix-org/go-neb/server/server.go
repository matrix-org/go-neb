// Package server contains building blocks for REST APIs.
package server

import (
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/errors"
	"net/http"
)

// JSONRequestHandler represents an interface that must be satisfied in order to respond to incoming
// HTTP requests with JSON. The interface returned will be marshalled into JSON to be sent to the client,
// unless the interface is []byte in which case the bytes are sent to the client unchanged.
// If an error is returned, a JSON error response will also be returned, unless the error code
// is a 302 REDIRECT in which case a redirect is sent based on the Message field.
type JSONRequestHandler interface {
	OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError)
}

// JSONError represents a JSON API error response
type JSONError struct {
	Message string `json:"message"`
}

// WithCORSOptions intercepts all OPTIONS requests and responds with CORS headers. The request handler
// is not invoked when this happens.
func WithCORSOptions(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "OPTIONS" {
			SetCORSHeaders(w)
			return
		}
		handler(w, req)
	}
}

// MakeJSONAPI creates an HTTP handler which always responds to incoming requests with JSON responses.
func MakeJSONAPI(handler JSONRequestHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		log.WithFields(log.Fields{
			"method": req.Method,
			"url":    req.URL,
		}).Print("Received request")
		res, httpErr := handler.OnIncomingRequest(req)

		// Set common headers returned regardless of the outcome of the request
		w.Header().Set("Content-Type", "application/json")
		SetCORSHeaders(w)

		if httpErr != nil {
			jsonErrorResponse(w, req, httpErr)
			return
		}

		// if they've returned bytes as the response, then just return them rather than marshalling as JSON.
		// This gives handlers an escape hatch if they want to return cached bytes.
		var resBytes []byte
		resBytes, ok := res.([]byte)
		if !ok {
			r, err := json.Marshal(res)
			if err != nil {
				jsonErrorResponse(w, req, &errors.HTTPError{nil, "Failed to serialise response as JSON", 500})
				return
			}
			resBytes = r
		}
		w.Write(resBytes)
	}
}

func jsonErrorResponse(w http.ResponseWriter, req *http.Request, httpErr *errors.HTTPError) {
	if httpErr.Code == 302 {
		log.WithField("err", httpErr.Error()).Print("Redirecting")
		http.Redirect(w, req, httpErr.Message, 302)
		return
	}

	log.WithField("err", httpErr.Error()).Print("Request failed")
	log.WithFields(log.Fields{
		"url":     req.URL,
		"code":    httpErr.Code,
		"message": httpErr.Message,
	}).Print("Responding with error")

	w.WriteHeader(httpErr.Code) // Set response code

	r, err := json.Marshal(&JSONError{
		Message: httpErr.Message,
	})
	if err != nil {
		// We should never fail to marshal the JSON error response, but in this event just skip
		// marshalling altogether
		log.Warn("Failed to marshal error response")
		w.Write([]byte(`{}`))
		return
	}
	w.Write(r)
}

// SetCORSHeaders sets unrestricted origin Access-Control headers on the response writer
func SetCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept")
}
