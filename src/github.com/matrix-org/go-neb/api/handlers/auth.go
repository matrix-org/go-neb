package handlers

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/errors"
	"github.com/matrix-org/go-neb/metrics"
	"github.com/matrix-org/go-neb/types"
)

type RequestAuthSession struct {
	Db *database.ServiceDB
}

func (h *RequestAuthSession) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}
	var body struct {
		RealmID string
		UserID  string
		Config  json.RawMessage
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing request JSON", 400}
	}
	log.WithFields(log.Fields{
		"realm_id": body.RealmID,
		"user_id":  body.UserID,
	}).Print("Incoming auth session request")

	if body.UserID == "" || body.RealmID == "" || body.Config == nil {
		return nil, &errors.HTTPError{nil, `Must supply a "UserID", a "RealmID" and a "Config"`, 400}
	}

	realm, err := h.Db.LoadAuthRealm(body.RealmID)
	if err != nil {
		return nil, &errors.HTTPError{err, "Unknown RealmID", 400}
	}

	response := realm.RequestAuthSession(body.UserID, body.Config)
	if response == nil {
		return nil, &errors.HTTPError{nil, "Failed to request auth session", 500}
	}

	metrics.IncrementAuthSession(realm.Type())

	return response, nil
}

type RemoveAuthSession struct {
	Db *database.ServiceDB
}

func (h *RemoveAuthSession) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}
	var body struct {
		RealmID string
		UserID  string
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing request JSON", 400}
	}
	log.WithFields(log.Fields{
		"realm_id": body.RealmID,
		"user_id":  body.UserID,
	}).Print("Incoming remove auth session request")

	if body.UserID == "" || body.RealmID == "" {
		return nil, &errors.HTTPError{nil, `Must supply a "UserID", a "RealmID"`, 400}
	}

	_, err := h.Db.LoadAuthRealm(body.RealmID)
	if err != nil {
		return nil, &errors.HTTPError{err, "Unknown RealmID", 400}
	}

	if err := h.Db.RemoveAuthSession(body.RealmID, body.UserID); err != nil {
		return nil, &errors.HTTPError{err, "Failed to remove auth session", 500}
	}

	return []byte(`{}`), nil
}

type RealmRedirect struct {
	Db *database.ServiceDB
}

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

type ConfigureAuthRealm struct {
	Db *database.ServiceDB
}

func (h *ConfigureAuthRealm) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}
	var body api.ConfigureAuthRealmRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing request JSON", 400}
	}

	if err := body.Check(); err != nil {
		return nil, &errors.HTTPError{err, err.Error(), 400}
	}

	realm, err := types.CreateAuthRealm(body.ID, body.Type, body.Config)
	if err != nil {
		return nil, &errors.HTTPError{err, "Error parsing config JSON", 400}
	}

	if err = realm.Register(); err != nil {
		return nil, &errors.HTTPError{err, "Error registering auth realm", 400}
	}

	oldRealm, err := h.Db.StoreAuthRealm(realm)
	if err != nil {
		return nil, &errors.HTTPError{err, "Error storing realm", 500}
	}

	return &struct {
		ID        string
		Type      string
		OldConfig types.AuthRealm
		NewConfig types.AuthRealm
	}{body.ID, body.Type, oldRealm, realm}, nil
}

type GetSession struct {
	Db *database.ServiceDB
}

func (h *GetSession) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}
	var body struct {
		RealmID string
		UserID  string
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing request JSON", 400}
	}

	if body.RealmID == "" || body.UserID == "" {
		return nil, &errors.HTTPError{nil, `Must supply a "RealmID" and "UserID"`, 400}
	}

	session, err := h.Db.LoadAuthSessionByUser(body.RealmID, body.UserID)
	if err != nil && err != sql.ErrNoRows {
		return nil, &errors.HTTPError{err, `Failed to load session`, 500}
	}
	if err == sql.ErrNoRows {
		return &struct {
			Authenticated bool
		}{false}, nil
	}

	return &struct {
		ID            string
		Authenticated bool
		Info          interface{}
	}{session.ID(), session.Authenticated(), session.Info()}, nil
}
