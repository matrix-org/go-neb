package main

import (
	"encoding/json"
	"github.com/matrix-org/go-neb/errors"
	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/database"
	"net/http"
)

type heartbeatHandler struct{}

func (*heartbeatHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	return &struct{}{}, nil
}

type configureClientHandler struct {
	db      *database.ServiceDB
	clients *clients.Clients
}

func (s *configureClientHandler) OnIncomingRequest(req *http.Request) (interface{}, *errors.HTTPError) {
	if req.Method != "POST" {
		return nil, &errors.HTTPError{nil, "Unsupported Method", 405}
	}

	var body database.ClientConfig
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
		OldClient database.ClientConfig
		NewClient database.ClientConfig
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

	service := database.CreateService(body.ID, body.Type)
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
		OldConfig database.Service
		NewConfig database.Service
	}{body.ID, body.Type, oldService, service}, nil
}
