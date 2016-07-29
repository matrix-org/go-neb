package services

import (
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"strings"
)

type echoService struct {
	id     string
	UserID string
	Rooms  []string
}

func (e *echoService) ServiceUserID() string { return e.UserID }
func (e *echoService) ServiceID() string     { return e.id }
func (e *echoService) ServiceType() string   { return "echo" }
func (e *echoService) RoomIDs() []string     { return e.Rooms }
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

func init() {
	database.RegisterService(func(serviceID string) database.Service {
		return &echoService{id: serviceID}
	})
}
