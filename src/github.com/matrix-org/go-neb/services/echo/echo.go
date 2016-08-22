package services

import (
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"github.com/matrix-org/go-neb/types"
	"net/http"
	"strings"
)

type echoService struct {
	id            string
	serviceUserID string
}

func (e *echoService) ServiceUserID() string                   { return e.serviceUserID }
func (e *echoService) ServiceID() string                       { return e.id }
func (e *echoService) ServiceType() string                     { return "echo" }
func (e *echoService) Register(oldService types.Service) error { return nil }
func (e *echoService) Plugin(roomID string) plugin.Plugin {
	return plugin.Plugin{
		Commands: []plugin.Command{
			plugin.Command{
				Path: []string{"echo"},
				Command: func(roomID, userID string, args []string) (interface{}, error) {
					return &matrix.TextMessage{"m.notice", strings.Join(args, " ")}, nil
				},
			},
		},
	}
}
func (e *echoService) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client) {
	w.WriteHeader(200) // Do nothing
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &echoService{id: serviceID, serviceUserID: serviceUserID}
	})
}
