package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/errors"
	"github.com/matrix-org/go-neb/types"
	"net/http"
	"strings"
	"sync"
)

type heartbeatHandler struct{}

func (*heartbeatHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	return &struct{}{}, nil
}

type requestAuthSessionHandler struct {
	db *database.ServiceDB
}

func (h *requestAuthSessionHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
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

	realm, err := h.db.LoadAuthRealm(body.RealmID)
	if err != nil {
		return nil, &errors.HTTPError{err, "Unknown RealmID", 400}
	}

	response := realm.RequestAuthSession(body.UserID, body.Config)
	if response == nil {
		return nil, &errors.HTTPError{nil, "Failed to request auth session", 500}
	}

	return response, nil
}

type realmRedirectHandler struct {
	db *database.ServiceDB
}

func (rh *realmRedirectHandler) handle(w http.ResponseWriter, req *http.Request) {
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

	realm, err := rh.db.LoadAuthRealm(realmID)
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

type configureAuthRealmHandler struct {
	db *database.ServiceDB
}

func (h *configureAuthRealmHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}
	var body struct {
		ID     string
		Type   string
		Config json.RawMessage
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing request JSON", 400}
	}

	if body.ID == "" || body.Type == "" || body.Config == nil {
		return nil, &errors.HTTPError{nil, `Must supply a "ID", a "Type" and a "Config"`, 400}
	}

	realm, err := types.CreateAuthRealm(body.ID, body.Type, body.Config)
	if err != nil {
		return nil, &errors.HTTPError{err, "Error parsing config JSON", 400}
	}

	if err = realm.Register(); err != nil {
		return nil, &errors.HTTPError{err, "Error registering auth realm", 400}
	}

	oldRealm, err := h.db.StoreAuthRealm(realm)
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

type webhookHandler struct {
	db      *database.ServiceDB
	clients *clients.Clients
}

func (wh *webhookHandler) handle(w http.ResponseWriter, req *http.Request) {
	log.WithField("path", req.URL.Path).Print("Incoming webhook request")
	segments := strings.Split(req.URL.Path, "/")
	// last path segment is the service ID which we will pass the incoming request to,
	// but we've base64d it.
	base64srvID := segments[len(segments)-1]
	bytesSrvID, err := base64.RawURLEncoding.DecodeString(base64srvID)
	srvID := string(bytesSrvID)
	if err != nil {
		log.WithError(err).WithField("base64_service_id", base64srvID).Print(
			"Not a b64 encoded string",
		)
		w.WriteHeader(400)
		return
	}

	service, err := wh.db.LoadService(srvID)
	if err != nil {
		log.WithError(err).WithField("service_id", srvID).Print("Failed to load service")
		w.WriteHeader(404)
		return
	}
	cli, err := wh.clients.Client(service.ServiceUserID())
	if err != nil {
		log.WithError(err).WithField("user_id", service.ServiceUserID()).Print(
			"Failed to retrieve matrix client instance")
		w.WriteHeader(500)
		return
	}
	log.WithFields(log.Fields{
		"service_id":  service.ServiceID(),
		"service_typ": service.ServiceType(),
	}).Print("Incoming webhook for service")
	service.OnReceiveWebhook(w, req, cli)
}

type configureClientHandler struct {
	db      *database.ServiceDB
	clients *clients.Clients
}

func (s *configureClientHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}

	var body types.ClientConfig
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing request JSON", 400}
	}

	if err := body.Check(); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing client config", 400}
	}

	oldClient, err := s.clients.Update(body)
	if err != nil {
		return nil, &errors.HTTPError{err, "Error storing token", 500}
	}

	return &struct {
		OldClient types.ClientConfig
		NewClient types.ClientConfig
	}{oldClient, body}, nil
}

type configureServiceHandler struct {
	db               *database.ServiceDB
	clients          *clients.Clients
	mapMutex         sync.Mutex
	mutexByServiceID map[string]*sync.Mutex
}

func newConfigureServiceHandler(db *database.ServiceDB, clients *clients.Clients) *configureServiceHandler {
	return &configureServiceHandler{
		db:               db,
		clients:          clients,
		mutexByServiceID: make(map[string]*sync.Mutex),
	}
}

func (s *configureServiceHandler) getMutexForServiceID(serviceID string) *sync.Mutex {
	s.mapMutex.Lock()
	defer s.mapMutex.Unlock()
	m := s.mutexByServiceID[serviceID]
	if m == nil {
		m = &sync.Mutex{}
		s.mutexByServiceID[serviceID] = m
	}
	return m
}

func (s *configureServiceHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}

	service, httpErr := s.createService(req)
	if httpErr != nil {
		return nil, httpErr
	}

	// Have mutexes around each service to queue up multiple requests for the same service ID
	mut := s.getMutexForServiceID(service.ServiceID())
	mut.Lock()
	defer mut.Unlock()

	old, err := s.db.LoadService(service.ServiceID())
	if err != nil && err != sql.ErrNoRows {
		return nil, &errors.HTTPError{err, "Error loading old service", 500}
	}

	if err = service.Register(old); err != nil {
		return nil, &errors.HTTPError{err, "Failed to register service: " + err.Error(), 500}
	}

	client, err := s.clients.Client(service.ServiceUserID())
	if err != nil {
		return nil, &errors.HTTPError{err, "Unknown matrix client", 400}
	}

	oldService, err := s.db.StoreService(service, client)
	if err != nil {
		return nil, &errors.HTTPError{err, "Error storing service", 500}
	}

	return &struct {
		ID        string
		Type      string
		OldConfig types.Service
		NewConfig types.Service
	}{service.ServiceID(), service.ServiceType(), oldService, service}, nil
}

func (s *configureServiceHandler) createService(req *http.Request) (types.Service, *errors.HTTPError) {
	var body struct {
		ID     string
		Type   string
		UserID string
		Config json.RawMessage
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing request JSON", 400}
	}

	if body.ID == "" || body.Type == "" || body.UserID == "" || body.Config == nil {
		return nil, &errors.HTTPError{
			nil, `Must supply an "ID", a "Type", a "UserID" and a "Config"`, 400,
		}
	}

	service, err := types.CreateService(body.ID, body.Type, body.UserID, body.Config)
	if err != nil {
		return nil, &errors.HTTPError{err, "Error parsing config JSON", 400}
	}
	return service, nil
}

type getServiceHandler struct {
	db *database.ServiceDB
}

func (h *getServiceHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}
	var body struct {
		ID string
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing request JSON", 400}
	}

	if body.ID == "" {
		return nil, &errors.HTTPError{nil, `Must supply a "ID"`, 400}
	}

	srv, err := h.db.LoadService(body.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, &errors.HTTPError{err, `Service not found`, 404}
		}
		return nil, &errors.HTTPError{err, `Failed to load service`, 500}
	}

	return &struct {
		ID     string
		Type   string
		Config types.Service
	}{srv.ServiceID(), srv.ServiceType(), srv}, nil
}

type getSessionHandler struct {
	db *database.ServiceDB
}

func (h *getSessionHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
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

	session, err := h.db.LoadAuthSessionByUser(body.RealmID, body.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, &errors.HTTPError{err, `Session not found`, 404}
		}
		return nil, &errors.HTTPError{err, `Failed to load session`, 500}
	}

	return &struct {
		ID            string
		Authenticated bool
		Info          interface{}
	}{session.ID(), session.Authenticated(), session.Info()}, nil
}
