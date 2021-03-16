package mumble

import (
	"crypto/tls"
	"fmt"
	"layeh.com/gumble/gumble"
	"layeh.com/gumble/gumbleutil"
	"net"

	"github.com/matrix-org/go-neb/types"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// ServiceType of the Mumble service
const ServiceType = "mumble"

type Service struct {
	types.DefaultService
	Endpoint string `json:"endpoint"`
	Insecure bool   `json:"insecure"`
	Username string `json:"username"`
	Room     string `json:"room"`
}

func (s *Service) Register(oldService types.Service, client types.MatrixClient) error {

	config := gumble.NewConfig()
	config.Username = s.Username

	config.Attach(gumbleutil.Listener{
		UserChange: func(e *gumble.UserChangeEvent) {
			if e.Type.Has(gumble.UserChangeConnected) {
				msg := mevt.MessageEventContent{
					Body:    fmt.Sprintf("User %s has joined Mumble!", e.User.Name),
					MsgType: "m.notice",
				}
				client.SendMessageEvent(id.RoomID(s.Room), mevt.EventMessage, msg)
			} else if e.Type.Has(gumble.UserChangeDisconnected) {
				msg := mevt.MessageEventContent{
					Body:    fmt.Sprintf("User %s has left Mumble", e.User.Name),
					MsgType: "m.notice",
				}
				client.SendMessageEvent(id.RoomID(s.Room), mevt.EventMessage, msg)
			}
		},
	})
	var tlsConfig tls.Config
	if s.Insecure {
		tlsConfig = tls.Config{
			InsecureSkipVerify: true,
		}
	} else {
		tlsConfig = tls.Config{}
	}

	_, err := gumble.DialWithDialer(new(net.Dialer), s.Endpoint, config, &tlsConfig)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	types.RegisterService(func(serviceID string, serviceUserID id.UserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
