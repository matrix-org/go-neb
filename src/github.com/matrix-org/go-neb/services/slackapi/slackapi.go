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
	// maps from hookID -> roomID
	Hooks map[string]struct {
		RoomID      string `json:"room_id"`
		MessageType string `json:"message_type"`
	}  `json:"hooks"`
}

func (s *Service) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client) {
	segments := strings.Split(req.URL.Path, "/")

	if len(segments) < 2 {
		w.WriteHeader(400)
		return
	}

	hookID := segments[len(segments)-2]
	messageType := s.Hooks[hookID].MessageType
	if messageType == "" {
		messageType = "m.text"
	}
	roomID := s.Hooks[hookID].RoomID

	slackMessage, err := getSlackMessage(*req)
	if err != nil {
		log.WithFields(log.Fields{"slackMessage":slackMessage, "err":err}).Print("Slack message error")
		w.WriteHeader(500)
		return
	}

	htmlMessage, err := slackMessageToHTMLMessage(slackMessage)
	if err != nil {
		log.WithField("err", err).Error("Converting slack message to HTML")
		w.WriteHeader(500)
		return
	}
	htmlMessage.MsgType = messageType
	cli.SendMessageEvent(
		roomID, "m.room.message", htmlMessage,
	)
	w.WriteHeader(200)
}

// Register joins all configured rooms
func (s *Service) Register(oldService types.Service, client *matrix.Client) error {
	for _, mapping := range s.Hooks {
		if _, err := client.JoinRoom(mapping.RoomID, "", ""); err != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: err,
				"room_id":    mapping.RoomID,
				"user_id":    client.UserID,
			}).Error("Failed to join room")
		}
	}
	return nil
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
