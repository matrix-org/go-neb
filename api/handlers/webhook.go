package handlers

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/metrics"
	log "github.com/sirupsen/logrus"
)

// Webhook represents an HTTP handler capable of accepting webhook requests on behalf of services.
type Webhook struct {
	db      *database.ServiceDB
	clients *clients.Clients
}

// NewWebhook returns a new webhook HTTP handler
func NewWebhook(db *database.ServiceDB, cli *clients.Clients) *Webhook {
	return &Webhook{db, cli}
}

// Handle an incoming webhook HTTP request.
//
// The webhook MUST have a known base64 encoded service ID as the last path segment
// in order for this request to be passed to the correct service, or else this will return
// HTTP 400. If the base64 encoded service ID is unknown, this will return HTTP 404.
// Beyond this, the exact response is determined by the specific Service implementation.
func (wh *Webhook) Handle(w http.ResponseWriter, req *http.Request) {
	log.WithField("path", req.URL.Path).Print("Incoming webhook request")
	segments := strings.Split(req.URL.Path, "/")
	// last path segment is the service ID which we will pass the incoming request to,
	// but we've base64d it.
	base64srvID := segments[len(segments)-1]
	bytesSrvID, err := base64.RawURLEncoding.DecodeString(base64srvID)
	if err != nil {
		log.WithError(err).WithField("base64_service_id", base64srvID).Print(
			"Not a b64 encoded string",
		)
		w.WriteHeader(400)
		return
	}
	srvID := string(bytesSrvID)

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
		"service_id":   service.ServiceID(),
		"service_type": service.ServiceType(),
	}).Print("Incoming webhook for service")
	metrics.IncrementWebhook(service.ServiceType())
	service.OnReceiveWebhook(w, req, cli)
}
