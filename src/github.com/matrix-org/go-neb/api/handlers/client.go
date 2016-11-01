package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/errors"
)

// ConfigureClient represents an HTTP handler capable of processing /configureClient requests
type ConfigureClient struct {
	Clients *clients.Clients
}

func (s *ConfigureClient) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}

	var body api.ClientConfig
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing request JSON", 400}
	}

	if err := body.Check(); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing client config", 400}
	}

	oldClient, err := s.Clients.Update(body)
	if err != nil {
		return nil, &errors.HTTPError{err, "Error storing token", 500}
	}

	return &struct {
		OldClient api.ClientConfig
		NewClient api.ClientConfig
	}{oldClient, body}, nil
}
