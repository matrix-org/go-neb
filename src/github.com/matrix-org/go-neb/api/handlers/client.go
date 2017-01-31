package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/util"
)

// ConfigureClient represents an HTTP handler capable of processing /admin/configureClient requests.
type ConfigureClient struct {
	Clients *clients.Clients
}

// OnIncomingRequest handles POST requests to /admin/configureClient. The JSON object provided
// is of type "api.ClientConfig".
//
// If a DisplayName is supplied, this request will set this client's display name
// if the old ClientConfig DisplayName differs from the new ClientConfig DisplayName.
//
// Request:
//  POST /admin/configureClient
//  {
//      "UserID": "@my_bot:localhost",
//      "HomeserverURL": "http://localhost:8008",
//      "Sync": true,
//      "DisplayName": "My Bot"
//  }
//
// Response:
//  HTTP/1.1 200 OK
//  {
//       "OldClient": {
//         // The old api.ClientConfig
//       },
//       "NewClient": {
//         // The new api.ClientConfig
//       }
//  }
func (s *ConfigureClient) OnIncomingRequest(req *http.Request) (interface{}, *util.HTTPError) {
	if req.Method != "POST" {
		return nil, &util.HTTPError{nil, "Unsupported Method", 405}
	}

	var body api.ClientConfig
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, &util.HTTPError{err, "Error parsing request JSON", 400}
	}

	if err := body.Check(); err != nil {
		return nil, &util.HTTPError{err, "Error parsing client config", 400}
	}

	oldClient, err := s.Clients.Update(body)
	if err != nil {
		return nil, &util.HTTPError{err, "Error storing token", 500}
	}

	return &struct {
		OldClient api.ClientConfig
		NewClient api.ClientConfig
	}{oldClient, body}, nil
}
