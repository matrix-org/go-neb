package slackapi

import (
	"net/http"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/types"
)

// ServiceType of the Slack API service
const ServiceType = "slackapi"

type Service struct {
	types.DefaultService
	webhookEndpointURL string
	// The URL which should be given to an outgoing slack webhook - Populated by Go-NEB after Service registration.
	WebhookURL string `json:"webhook_url"`
	RoomID      string `json:"room_id"`
	MessageType string `json:"message_type"`
}

func (s *Service) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client) {
	segments := strings.Split(req.URL.Path, "/")

	if len(segments) < 2 {
		w.WriteHeader(400)
		return
	}

	messageType := s.MessageType
	if messageType == "" {
		messageType = "m.text"
	}
	roomID := s.RoomID

	slackMessage, err := getSlackMessage(*req)
	if err != nil {
		log.WithFields(log.Fields{"slackMessage":slackMessage, log.ErrorKey:err}).Error("Slack message error")
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
		roomID, "m.room.message", htmlMessage,
	)
	w.WriteHeader(200)
}

// Register joins the configured room and sets the public WebhookURL
func (s *Service) Register(oldService types.Service, client *matrix.Client) error {
	s.WebhookURL = s.webhookEndpointURL
	if _, err := client.JoinRoom(s.RoomID, "", ""); err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey: err,
			"room_id":    s.RoomID,
			"user_id":    client.UserID,
		}).Error("Failed to join room")
	}
	return nil
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
			webhookEndpointURL: webhookEndpointURL,
		}
	})
}
