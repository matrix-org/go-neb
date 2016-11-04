package slackapi

import (
	"net/http"
	"strings"

	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/types"
)

// ServiceType of the Slack API service
const ServiceType = "slackapi"

type Service struct {
	types.DefaultService
	ClientUserID string
	// maps from hookID -> roomID
	Hooks map[string]struct {
		RoomID      string
		MessageType string
	}
}

func (s *Service) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client) {
	segments := strings.Split(req.URL.Path, "/")

	if len(segments) < 2 {
		w.WriteHeader(400)
	}

	hookID := segments[len(segments)-2]
	messageType := s.Hooks[hookID].MessageType
	if messageType == "" {
		messageType = "m.text"
	}
	roomID := s.Hooks[hookID].RoomID

	slackMessage, err := getSlackMessage(*req)
	if err != nil {
		return
	}
	htmlMessage, err := slackMessageToHTMLMessage(slackMessage)
	if err != nil {
		return
	}
	htmlMessage.MsgType = messageType
	cli.SendMessageEvent(
		roomID,
		"m.room.message",
		htmlMessage)
	w.WriteHeader(200)
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
