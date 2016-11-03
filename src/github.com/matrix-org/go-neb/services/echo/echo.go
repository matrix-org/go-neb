package services

import (
	"strings"

	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/types"
)

type echoService struct {
	types.DefaultService
	id            string
	serviceUserID string
}

func (e *echoService) ServiceUserID() string { return e.serviceUserID }
func (e *echoService) ServiceID() string     { return e.id }
func (e *echoService) ServiceType() string   { return "echo" }
func (e *echoService) Commands(cli *matrix.Client, roomID string) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"echo"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return &matrix.TextMessage{"m.notice", strings.Join(args, " ")}, nil
			},
		},
	}
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &echoService{id: serviceID, serviceUserID: serviceUserID}
	})
}
