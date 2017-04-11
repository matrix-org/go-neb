// Package riotbot implements a Service for user onboarding in Riot.
package riotbot

import (
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

// ServiceType of the Riotbot service
const ServiceType = "riotbot"

// Service represents the Riotbot service. It has no Config fields.
type Service struct {
	types.DefaultService
}

// Commands supported:
//    !help some request
// Responds with some user help.
func (e *Service) Commands(cli *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"help"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return &gomatrix.TextMessage{"m.notice", "I can't help you with that"}, nil
			},
		},
	}
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
