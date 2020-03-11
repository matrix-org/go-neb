package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/metrics"
	"github.com/matrix-org/go-neb/polling"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
	"github.com/matrix-org/util"
	log "github.com/sirupsen/logrus"
)

// ConfigureService represents an HTTP handler which can process /admin/configureService requests.
type ConfigureService struct {
	db               *database.ServiceDB
	clients          *clients.Clients
	mapMutex         sync.Mutex
	mutexByServiceID map[string]*sync.Mutex
}

// NewConfigureService creates a new ConfigureService handler
func NewConfigureService(db *database.ServiceDB, clients *clients.Clients) *ConfigureService {
	return &ConfigureService{
		db:               db,
		clients:          clients,
		mutexByServiceID: make(map[string]*sync.Mutex),
	}
}

func (s *ConfigureService) getMutexForServiceID(serviceID string) *sync.Mutex {
	s.mapMutex.Lock()
	defer s.mapMutex.Unlock()
	m := s.mutexByServiceID[serviceID]
	if m == nil {
		// XXX TODO: There's a memory leak here. The amount of mutexes created is unbounded, as there will be 1 per service which are never deleted.
		// A better solution would be to have a striped hash map with a bounded pool of mutexes. We can't live with a single global mutex because the Register()
		// function this is protecting does many many HTTP requests which can take a long time on bad networks and will head of line block other services.
		m = &sync.Mutex{}
		s.mutexByServiceID[serviceID] = m
	}
	return m
}

// OnIncomingRequest handles POST requests to /admin/configureService.
//
// The request body MUST be of type "api.ConfigureServiceRequest".
//
// Request:
//  POST /admin/configureService
//  {
//      "ID": "my_service_id",
//      "Type": "service-type",
//      "UserID": "@my_bot:localhost",
//      "Config": {
//          // service-specific config information
//      }
//  }
// Response:
//  HTTP/1.1 200 OK
//  {
//      "ID": "my_service_id",
//      "Type": "service-type",
//      "OldConfig": {
//          // old service-specific config information
//      },
//      "NewConfig": {
//          // new service-specific config information
//      },
//  }
func (s *ConfigureService) OnIncomingRequest(req *http.Request) util.JSONResponse {
	if req.Method != "POST" {
		return util.MessageResponse(405, "Unsupported Method")
	}

	service, httpErr := s.createService(req)
	if httpErr != nil {
		return *httpErr
	}
	logger := util.GetLogger(req.Context())
	logger.WithFields(log.Fields{
		"service_id":      service.ServiceID(),
		"service_type":    service.ServiceType(),
		"service_user_id": service.ServiceUserID(),
	}).Print("Incoming configure service request")

	// Have mutexes around each service to queue up multiple requests for the same service ID
	mut := s.getMutexForServiceID(service.ServiceID())
	mut.Lock()
	defer mut.Unlock()

	old, err := s.db.LoadService(service.ServiceID())
	if err != nil && err != sql.ErrNoRows {
		logger.WithError(err).Error("Failed to LoadService")
		return util.MessageResponse(500, "Error loading old service")
	}

	client, err := s.clients.Client(service.ServiceUserID())
	if err != nil {
		return util.MessageResponse(400, "Unknown matrix client")
	}

	if err := checkClientForService(service, client); err != nil {
		return util.MessageResponse(400, err.Error())
	}

	if err = service.Register(old, client); err != nil {
		return util.MessageResponse(500, "Failed to register service: "+err.Error())
	}

	oldService, err := s.db.StoreService(service)
	if err != nil {
		logger.WithError(err).Error("Failed to StoreService")
		return util.MessageResponse(500, "Error storing service")
	}

	// Start any polling NOW because they may decide to stop it in PostRegister, and we want to make
	// sure we'll actually stop.
	if _, ok := service.(types.Poller); ok {
		if err := polling.StartPolling(service); err != nil {
			logger.WithFields(log.Fields{
				"service_id": service.ServiceID(),
				log.ErrorKey: err,
			}).Error("Failed to start poll loop.")
		}
	}

	service.PostRegister(old)
	metrics.IncrementConfigureService(service.ServiceType())

	return util.JSONResponse{
		Code: 200,
		JSON: struct {
			ID        string
			Type      string
			OldConfig types.Service
			NewConfig types.Service
		}{service.ServiceID(), service.ServiceType(), oldService, service},
	}
}

func (s *ConfigureService) createService(req *http.Request) (types.Service, *util.JSONResponse) {
	var body api.ConfigureServiceRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		res := util.MessageResponse(400, "Error parsing request JSON")
		return nil, &res
	}

	if err := body.Check(); err != nil {
		res := util.MessageResponse(400, err.Error())
		return nil, &res
	}

	service, err := types.CreateService(body.ID, body.Type, body.UserID, body.Config)
	if err != nil {
		res := util.MessageResponse(400, "Error parsing config JSON")
		return nil, &res
	}
	return service, nil
}

// GetService represents an HTTP handler which can process /admin/getService requests.
type GetService struct {
	Db *database.ServiceDB
}

// OnIncomingRequest handles POST requests to /admin/getService.
//
// The request body MUST be a JSON body which has an "ID" key which represents
// the service ID to get.
//
// Request:
//  POST /admin/getService
//  {
//      "ID": "my_service_id"
//  }
// Response:
//  HTTP/1.1 200 OK
//  {
//      "ID": "my_service_id",
//      "Type": "github",
//      "Config": {
//          // service-specific config information
//      }
//  }
func (h *GetService) OnIncomingRequest(req *http.Request) util.JSONResponse {
	if req.Method != "POST" {
		return util.MessageResponse(405, "Unsupported Method")
	}
	var body struct {
		ID string
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return util.MessageResponse(400, "Error parsing request JSON")
	}

	if body.ID == "" {
		return util.MessageResponse(400, `Must supply a "ID"`)
	}

	srv, err := h.Db.LoadService(body.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return util.MessageResponse(404, `Service not found`)
		}
		util.GetLogger(req.Context()).WithError(err).Error("Failed to LoadService")
		return util.MessageResponse(500, `Failed to load service`)
	}

	return util.JSONResponse{
		Code: 200,
		JSON: struct {
			ID     string
			Type   string
			Config types.Service
		}{srv.ServiceID(), srv.ServiceType(), srv},
	}
}

func checkClientForService(service types.Service, client *gomatrix.Client) error {
	// If there are any commands or expansions for this Service then the service user ID
	// MUST be a syncing client or else the Service will never get the incoming command/expansion!
	cmds := service.Commands(client)
	expans := service.Expansions(client)
	if len(cmds) > 0 || len(expans) > 0 {
		nebStore := client.Store.(*matrix.NEBStore)
		if !nebStore.ClientConfig.Sync {
			return fmt.Errorf(
				"Service type '%s' requires a syncing client", service.ServiceType(),
			)
		}
	}
	return nil
}
