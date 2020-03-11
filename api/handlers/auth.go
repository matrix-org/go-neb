package handlers

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/metrics"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/util"
	log "github.com/sirupsen/logrus"
)

// RequestAuthSession represents an HTTP handler capable of processing /admin/requestAuthSession requests.
type RequestAuthSession struct {
	Db *database.ServiceDB
}

// OnIncomingRequest handles POST requests to /admin/requestAuthSession. The HTTP body MUST be
// a JSON object representing type "api.RequestAuthSessionRequest".
//
// This will return HTTP 400 if there are missing fields or the Realm ID is unknown.
// For the format of the response, see the specific AuthRealm that the Realm ID corresponds to.
//
// Request:
//  POST /admin/requestAuthSession
//  {
//      "RealmID": "github_realm_id",
//      "UserID": "@my_user:localhost",
//      "Config": {
//          // AuthRealm specific config info
//      }
//  }
// Response:
//  HTTP/1.1 200 OK
//  {
//      // AuthRealm-specific information
//  }
func (h *RequestAuthSession) OnIncomingRequest(req *http.Request) util.JSONResponse {
	logger := util.GetLogger(req.Context())
	if req.Method != "POST" {
		return util.MessageResponse(405, "Unsupported Method")
	}
	var body api.RequestAuthSessionRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return util.MessageResponse(400, "Error parsing request JSON")
	}
	logger.WithFields(log.Fields{
		"realm_id": body.RealmID,
		"user_id":  body.UserID,
	}).Print("Incoming auth session request")

	if err := body.Check(); err != nil {
		logger.WithError(err).Info("Failed Check")
		return util.MessageResponse(400, err.Error())
	}

	realm, err := h.Db.LoadAuthRealm(body.RealmID)
	if err != nil {
		logger.WithError(err).Info("Failed to LoadAuthRealm")
		return util.MessageResponse(400, "Unknown RealmID")
	}

	response := realm.RequestAuthSession(body.UserID, body.Config)
	if response == nil {
		logger.WithField("body", body).Error("Failed to RequestAuthSession")
		return util.MessageResponse(500, "Failed to request auth session")
	}

	metrics.IncrementAuthSession(realm.Type())

	return util.JSONResponse{
		Code: 200,
		JSON: response,
	}
}

// RemoveAuthSession represents an HTTP handler capable of processing /admin/removeAuthSession requests.
type RemoveAuthSession struct {
	Db *database.ServiceDB
}

// OnIncomingRequest handles POST requests to /admin/removeAuthSession.
//
// The JSON object MUST contain the keys "RealmID" and "UserID" to identify the session to remove.
//
// Request
//  POST /admin/removeAuthSession
//  {
//      "RealmID": "github-realm",
//      "UserID": "@my_user:localhost"
//  }
// Response:
//  HTTP/1.1 200 OK
//  {}
func (h *RemoveAuthSession) OnIncomingRequest(req *http.Request) util.JSONResponse {
	logger := util.GetLogger(req.Context())
	if req.Method != "POST" {
		return util.MessageResponse(405, "Unsupported Method")
	}
	var body struct {
		RealmID string
		UserID  string
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return util.MessageResponse(400, "Error parsing request JSON")
	}
	logger.WithFields(log.Fields{
		"realm_id": body.RealmID,
		"user_id":  body.UserID,
	}).Print("Incoming remove auth session request")

	if body.UserID == "" || body.RealmID == "" {
		return util.MessageResponse(400, `Must supply a "UserID", a "RealmID"`)
	}

	_, err := h.Db.LoadAuthRealm(body.RealmID)
	if err != nil {
		return util.MessageResponse(400, "Unknown RealmID")
	}

	if err := h.Db.RemoveAuthSession(body.RealmID, body.UserID); err != nil {
		logger.WithError(err).Error("Failed to RemoveAuthSession")
		return util.MessageResponse(500, "Failed to remove auth session")
	}

	return util.JSONResponse{
		Code: 200,
		JSON: struct{}{},
	}
}

// RealmRedirect represents an HTTP handler which can process incoming redirects for auth realms.
type RealmRedirect struct {
	Db *database.ServiceDB
}

// Handle requests for an auth realm.
//
// The last path segment of the URL MUST be the base64 form of the Realm ID. What response
// this returns depends on the specific AuthRealm implementation.
func (rh *RealmRedirect) Handle(w http.ResponseWriter, req *http.Request) {
	segments := strings.Split(req.URL.Path, "/")
	// last path segment is the base64d realm ID which we will pass the incoming request to
	base64realmID := segments[len(segments)-1]
	bytesRealmID, err := base64.RawURLEncoding.DecodeString(base64realmID)
	realmID := string(bytesRealmID)
	if err != nil {
		log.WithError(err).WithField("base64_realm_id", base64realmID).Print(
			"Not a b64 encoded string",
		)
		w.WriteHeader(400)
		return
	}

	realm, err := rh.Db.LoadAuthRealm(realmID)
	if err != nil {
		log.WithError(err).WithField("realm_id", realmID).Print("Failed to load realm")
		w.WriteHeader(404)
		return
	}
	log.WithFields(log.Fields{
		"realm_id": realmID,
	}).Print("Incoming realm redirect request")
	realm.OnReceiveRedirect(w, req)
}

// ConfigureAuthRealm represents an HTTP handler capable of processing /admin/configureAuthRealm requests.
type ConfigureAuthRealm struct {
	Db *database.ServiceDB
}

// OnIncomingRequest handles POST requests to /admin/configureAuthRealm. The JSON object
// provided is of type "api.ConfigureAuthRealmRequest".
//
// Request:
//  POST /admin/configureAuthRealm
//  {
//      "ID": "my-realm-id",
//      "Type": "github",
//      "Config": {
//          // Realm-specific configuration information
//      }
//  }
// Response:
//  HTTP/1.1 200 OK
//  {
//      "ID": "my-realm-id",
//      "Type": "github",
//      "OldConfig": {
//          // Old auth realm config information
//      },
//      "NewConfig": {
//          // New auth realm config information
//      },
//  }
func (h *ConfigureAuthRealm) OnIncomingRequest(req *http.Request) util.JSONResponse {
	logger := util.GetLogger(req.Context())
	if req.Method != "POST" {
		return util.MessageResponse(405, "Unsupported Method")
	}
	var body api.ConfigureAuthRealmRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return util.MessageResponse(400, "Error parsing request JSON")
	}

	if err := body.Check(); err != nil {
		return util.MessageResponse(400, err.Error())
	}

	realm, err := types.CreateAuthRealm(body.ID, body.Type, body.Config)
	if err != nil {
		return util.MessageResponse(400, "Error parsing config JSON")
	}

	if err = realm.Register(); err != nil {
		return util.MessageResponse(400, "Error registering auth realm")
	}

	oldRealm, err := h.Db.StoreAuthRealm(realm)
	if err != nil {
		logger.WithError(err).Error("Failed to StoreAuthRealm")
		return util.MessageResponse(500, "Error storing realm")
	}

	return util.JSONResponse{
		Code: 200,
		JSON: struct {
			ID        string
			Type      string
			OldConfig types.AuthRealm
			NewConfig types.AuthRealm
		}{body.ID, body.Type, oldRealm, realm},
	}
}

// GetSession represents an HTTP handler capable of processing /admin/getSession requests.
type GetSession struct {
	Db *database.ServiceDB
}

// OnIncomingRequest handles POST requests to /admin/getSession.
//
// The JSON object provided MUST have a "RealmID" and "UserID" in order to fetch the
// correct AuthSession. If there is no session for this tuple of realm and user ID,
// a 200 OK is still returned with "Authenticated" set to false.
//
// Request:
//  POST /admin/getSession
//  {
//      "RealmID": "my-realm",
//      "UserID": "@my_user:localhost"
//  }
// Response:
//  HTTP/1.1 200 OK
//  {
//      "ID": "session_id",
//      "Authenticated": true,
//      "Info": {
//          // Session-specific config info
//      }
//  }
// Response if session not found:
//  HTTP/1.1 200 OK
//  {
//      "Authenticated": false
//  }
func (h *GetSession) OnIncomingRequest(req *http.Request) util.JSONResponse {
	logger := util.GetLogger(req.Context())
	if req.Method != "POST" {
		return util.MessageResponse(405, "Unsupported Method")
	}
	var body struct {
		RealmID string
		UserID  string
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return util.MessageResponse(400, "Error parsing request JSON")
	}

	if body.RealmID == "" || body.UserID == "" {
		return util.MessageResponse(400, `Must supply a "RealmID" and "UserID"`)
	}

	session, err := h.Db.LoadAuthSessionByUser(body.RealmID, body.UserID)
	if err != nil && err != sql.ErrNoRows {
		logger.WithError(err).WithField("body", body).Error("Failed to LoadAuthSessionByUser")
		return util.MessageResponse(500, `Failed to load session`)
	}
	if err == sql.ErrNoRows {
		return util.JSONResponse{
			Code: 200,
			JSON: struct {
				Authenticated bool
			}{false},
		}
	}

	return util.JSONResponse{
		Code: 200,
		JSON: struct {
			ID            string
			Authenticated bool
			Info          interface{}
		}{session.ID(), session.Authenticated(), session.Info()},
	}
}
