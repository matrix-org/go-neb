package slackapi

import (
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"github.com/matrix-org/go-neb/types"
	"net/http"
	"strings"
)

type slackAPIService struct {
	id                 string
	serviceUserID      string
	webhookEndpointURL string
	ClientUserID       string
	// maps from hookID -> roomID
	Hooks map[string]struct {
		RoomID      string
		MessageType string
	}
}

func (s *slackAPIService) ServiceUserID() string                 { return s.serviceUserID }
func (s *slackAPIService) ServiceID() string                     { return s.id }
func (s *slackAPIService) ServiceType() string                   { return "slackapi" }
func (s *slackAPIService) PostRegister(oldService types.Service) {}
func (s *slackAPIService) Register(oldService types.Service, client *matrix.Client) error {
	return nil
}

func (s *slackAPIService) Plugin(cli *matrix.Client, roomID string) plugin.Plugin {
	return plugin.Plugin{}
}

func (s *slackAPIService) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client) {
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
		return &slackAPIService{
			id:                 serviceID,
			serviceUserID:      serviceUserID,
			webhookEndpointURL: webhookEndpointURL,
		}
	})
}
