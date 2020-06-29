// Package echo implements a Service which echoes back !commands.
package echo

import (
	"strings"

	"github.com/matrix-org/go-neb/types"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// ServiceType of the Echo service
const ServiceType = "echo"

// Service represents the Echo service. It has no Config fields.
type Service struct {
	types.DefaultService
}

// Commands supported:
//    !echo some message
// Responds with a notice of "some message".
func (e *Service) Commands(cli types.MatrixClient) []types.Command {
	return []types.Command{
		{
			Path: []string{"echo"},
			Command: func(roomID id.RoomID, userID id.UserID, args []string) (interface{}, error) {
				return &mevt.MessageEventContent{
					MsgType: mevt.MsgNotice,
					Body:    strings.Join(args, " "),
				}, nil
			},
		},
	}
}

func init() {
	types.RegisterService(func(serviceID string, serviceUserID id.UserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
