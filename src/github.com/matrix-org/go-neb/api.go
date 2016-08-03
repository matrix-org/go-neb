package main

import (
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/errors"
	"github.com/matrix-org/go-neb/types"
	"net/http"
	"strings"
)

type heartbeatHandler struct{}

func (*heartbeatHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	return &struct{}{}, nil
}

type configureAuthHandler struct {
	db *database.ServiceDB
}

func (*configureAuthHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}
	var tpa types.ThirdPartyAuth
	if err := json.NewDecoder(req.Body).Decode(&tpa); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing request JSON", 400}
	}

	am := types.GetAuthModule(tpa.Type)
	if am == nil {
		return nil, &errors.HTTPError{nil, "Bad auth type: " + tpa.Type, 400}
	}

	err := am.Process(tpa)
	if err != nil {
		return nil, &errors.HTTPError{err, "Failed to persist auth", 500}
	}

	return nil, nil
}

type webhookHandler struct {
	db      *database.ServiceDB
	clients *clients.Clients
}

func (wh *webhookHandler) handle(w http.ResponseWriter, req *http.Request) {
	segments := strings.Split(req.URL.Path, "/")
	// last path segment is the service ID which we will pass the incoming request to
	srvID := segments[len(segments)-1]
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
	db      *database.ServiceDB
	clients *clients.Clients
}

func (s *configureServiceHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
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

	service := types.CreateService(body.ID, body.Type)
	if service == nil {
		return nil, &errors.HTTPError{nil, "Unknown service type", 400}
	}

	if err := json.Unmarshal(body.Config, service); err != nil {
		return nil, &errors.HTTPError{err, "Error parsing config JSON", 400}
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
	}{body.ID, body.Type, oldService, service}, nil
}
