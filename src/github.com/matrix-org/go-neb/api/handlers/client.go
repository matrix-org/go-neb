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
func (s *ConfigureClient) OnIncomingRequest(req *http.Request) util.JSONResponse {
	if req.Method != "POST" {
		return util.MessageResponse(405, "Unsupported Method")
	}

	var body api.ClientConfig
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return util.MessageResponse(400, "Error parsing request JSON")
	}

	if err := body.Check(); err != nil {
		return util.MessageResponse(400, "Error parsing client config")
	}

	oldClient, err := s.Clients.Update(body)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).WithField("body", body).Error("Failed to Clients.Update")
		return util.MessageResponse(500, "Error storing token")
	}

	return util.JSONResponse{
		Code: 200,
		JSON: struct {
			OldClient api.ClientConfig
			NewClient api.ClientConfig
		}{oldClient, body},
	}
}
