package slackapi

import (
	"net/http"
	"strings"

	"github.com/matrix-org/go-neb/types"
	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// ServiceType of the Slack API service
const ServiceType = "slackapi"

// Service contains the Config fields for the Slack API service.
//
// This service will send HTML formatted messages into a room when an outgoing slack webhook
// hits WebhookURL.
//
// Example JSON request:
// {
//   "room_id": "!someroomid:some.domain.com",
//   "message_type": "m.text"
// }
type Service struct {
	types.DefaultService
	webhookEndpointURL string
	// The URL which should be given to an outgoing slack webhook - Populated by Go-NEB after Service registration.
	WebhookURL  string            `json:"webhook_url"`
	RoomID      id.RoomID         `json:"room_id"`
	MessageType event.MessageType `json:"message_type"`
}

// OnReceiveWebhook receives requests from a slack outgoing webhook and possibly sends requests
// to Matrix as a result.
//
// This requires that the WebhookURL is given to an outgoing slack webhook (see https://api.slack.com/outgoing-webhooks)
func (s *Service) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli types.MatrixClient) {
	segments := strings.Split(req.URL.Path, "/")

	if len(segments) < 2 {
		w.WriteHeader(400)
		return
	}

	messageType := s.MessageType
	if messageType == "" {
		messageType = event.MsgText
	}
	roomID := s.RoomID

	slackMessage, err := getSlackMessage(*req)
	if err != nil {
		log.WithFields(log.Fields{"slackMessage": slackMessage, log.ErrorKey: err}).Error("Slack message error")
		w.WriteHeader(500)
		return
	}

	htmlMessage, err := slackMessageToHTMLMessage(slackMessage)
	if err != nil {
		log.WithError(err).Error("Converting slack message to HTML")
		w.WriteHeader(500)
		return
	}
	htmlMessage.MsgType = messageType
	cli.SendMessageEvent(
		roomID, event.EventMessage, htmlMessage,
	)
	w.WriteHeader(200)
}

// Register joins the configured room and sets the public WebhookURL
func (s *Service) Register(oldService types.Service, client types.MatrixClient) error {
	s.WebhookURL = s.webhookEndpointURL
	if _, err := client.JoinRoom(s.RoomID.String(), "", nil); err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey: err,
			"room_id":    s.RoomID,
		}).Error("Failed to join room")
	}
	return nil
}

func init() {
	types.RegisterService(func(serviceID string, serviceUserID id.UserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService:     types.NewDefaultService(serviceID, serviceUserID, ServiceType),
			webhookEndpointURL: webhookEndpointURL,
		}
	})
}
