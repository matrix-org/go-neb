// Package echo implements a Service which echoes back !commands.
package echo

import (
	"fmt"
	"math/rand"
	"strconv"

	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/types"
	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// ServiceType of the Echo service
const ServiceType = "cryptotest"

var expectedString map[id.RoomID]string

// Service represents the Cryptotest service. It has no Config fields.
type Service struct {
	types.DefaultService
	Rooms []id.RoomID `json:"rooms"`
}

func randomString() (res string) {
	for i := 0; i < 10; i++ {
		res += string(rune(rand.Intn('Z'-'A') + 'A'))
	}
	return
}

func (s *Service) inRoom(roomID id.RoomID) bool {
	for _, joinedRoomID := range s.Rooms {
		if joinedRoomID == roomID {
			return true
		}
	}
	return false
}

func (s *Service) handleEventMessage(source mautrix.EventSource, evt *mevt.Event) {
	log.Infof("got a %v", evt.Content.AsMessage().Body)
}

// Commands supported:
//    !crypto_response random_string
// Responds with a notice of "some message".
func (s *Service) Commands(cli types.MatrixClient) []types.Command {
	botClient := cli.(*clients.BotClient)
	return []types.Command{
		{
			Path: []string{"crypto_help"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				if s.inRoom(roomID) {
					// TODO help msg
					return mevt.MessageEventContent{MsgType: mevt.MsgText, Body: "help"}, nil
				}
				return nil, nil
			},
		},
		{
			Path: []string{"crypto_challenge"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				if s.inRoom(roomID) {
					randStr := randomString()
					log.Infof("Setting challenge for room %v: %v", roomID, expectedString)
					expectedString[roomID] = randStr
					prefix := "!challenge"
					if len(arguments) > 0 {
						prefix = arguments[0]
					}
					return mevt.MessageEventContent{MsgType: mevt.MsgText, Body: fmt.Sprintf("%v %v", prefix, randStr)}, nil
				}
				return nil, nil
			},
		},
		{
			Path: []string{"crypto_response"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				if s.inRoom(roomID) {
					if len(arguments) > 0 && arguments[0] == expectedString[roomID] {
						return mevt.MessageEventContent{
							MsgType: mevt.MsgText,
							Body:    fmt.Sprintf("Correct response received from %v", userID.String()),
						}, nil
					} else {
						return mevt.MessageEventContent{
							MsgType: mevt.MsgText,
							Body:    fmt.Sprintf("Incorrect response received from %v", userID.String()),
						}, nil
					}
				}
				return nil, nil
			},
		},
		{
			Path: []string{"crypto_new_session"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				if s.inRoom(roomID) {
					sessionID, err := botClient.InvalidateRoomSession(roomID)
					if err != nil {
						log.WithField("room_id", roomID).Errorf("Error invalidating session ID: %v", err)
						return mevt.MessageEventContent{MsgType: mevt.MsgText, Body: fmt.Sprintf("Error invalidating session ID: %v", sessionID)}, nil
					}
					return mevt.MessageEventContent{
						MsgType: mevt.MsgText,
						Body:    fmt.Sprintf("Invalidated previous session ID (%v)", sessionID),
					}, nil
				}
				return nil, nil
			},
		},
		{
			Path: []string{"sas_verify_me"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				if s.inRoom(roomID) && len(arguments) > 0 {
					deviceID := id.DeviceID(arguments[0])
					transaction, err := botClient.StartSASVerification(userID, deviceID)
					if err != nil {
						log.WithFields(log.Fields{"user_id": userID, "device_id": deviceID}).WithError(err).Error("Error starting SAS verification")
						return mevt.MessageEventContent{
							MsgType: mevt.MsgText,
							Body:    fmt.Sprintf("Error starting SAS verification: %v", err),
						}, nil
					}
					return mevt.MessageEventContent{
						MsgType: mevt.MsgText,
						Body:    fmt.Sprintf("Started SAS verification with user %v device %v: transaction %v", userID, deviceID, transaction),
					}, nil
				}
				return nil, nil
			},
		},
		{
			Path: []string{"sas_decimal_code"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				if s.inRoom(roomID) && len(arguments) == 4 {
					deviceID := id.DeviceID(arguments[0])
					var decimalSAS crypto.DecimalSASData
					for i := 0; i < 3; i++ {
						sasCode, err := strconv.Atoi(arguments[i+1])
						if err != nil {
							log.WithFields(log.Fields{"user_id": userID, "device_id": deviceID}).WithError(err).Error("Error reading SAS code")
							return mevt.MessageEventContent{
								MsgType: mevt.MsgText,
								Body:    fmt.Sprintf("Error reading SAS cdoe: %v", err),
							}, nil
						}
						decimalSAS[i] = uint(sasCode)
					}
					botClient.SubmitDecimalSAS(userID, deviceID, decimalSAS)
					return mevt.MessageEventContent{
						MsgType: mevt.MsgText,
						Body:    fmt.Sprintf("Read SAS code from user %v device %v: %v", userID, deviceID, decimalSAS),
					}, nil
				}
				return nil, nil
			},
		},
	}
}

// Register registers
func (s *Service) Register(oldService types.Service, client types.MatrixClient) error {
	botClient := client.(*clients.BotClient)
	botClient.Syncer.(mautrix.ExtensibleSyncer).OnEventType(mevt.EventMessage, s.handleEventMessage)
	for _, roomID := range s.Rooms {
		if _, err := client.JoinRoom(roomID.String(), "", nil); err != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: err,
				"room_id":    roomID,
			}).Error("Failed to join room")
		}
	}
	return nil
}

func init() {
	expectedString = make(map[id.RoomID]string)
	types.RegisterService(func(serviceID string, serviceUserID id.UserID, webhookEndpointURL string) types.Service {
		s := &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
		return s
	})
}
