package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/util"
	"maunium.net/go/mautrix/crypto"
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

// VerifySAS represents an HTTP handler capable of processing /verifySAS requests.
type VerifySAS struct {
	Clients *clients.Clients
}

// OnIncomingRequest handles POST requests to /verifySAS. The JSON object provided
// is of type "api.IncomingDecimalSAS".
//
// The request should contain the three decimal SAS numbers as displayed on the other device that is being verified,
// as well as that device's user and device ID.
// It should also contain the user ID that Go-NEB's client is using.
//
// Request:
//  POST /verifySAS
//  {
//      "UserID": "@my_bot:localhost", // Neb's user ID
//      "OtherUserID": "@user:localhost", // User ID of device we're verifying with
//      "OtherDeviceID": "ABCDEFG", // Device ID of device we're verifying with
//      "SAS": [1111, 2222, 3333] // SAS displayed on device we're verifying with
//  }
//
// Response:
//  HTTP/1.1 200 OK
//  {}
func (s *VerifySAS) OnIncomingRequest(req *http.Request) util.JSONResponse {
	if req.Method != "POST" {
		return util.MessageResponse(405, "Unsupported Method")
	}

	var body api.IncomingDecimalSAS
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return util.MessageResponse(400, "Error parsing request JSON")
	}

	if err := body.Check(); err != nil {
		return util.MessageResponse(400, "Error parsing client config")
	}

	client, err := s.Clients.Client(body.UserID)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).WithField("body", body).Error("Failed to load client")
		return util.MessageResponse(500, "Error storing SAS")
	}

	client.SubmitDecimalSAS(body.OtherUserID, body.OtherDeviceID, crypto.DecimalSASData(body.SAS))

	return util.JSONResponse{
		Code: 200,
		JSON: struct{}{},
	}
}
