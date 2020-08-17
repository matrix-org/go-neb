// Package cryptotest implements a Service which provides several commands for testing the e2e functionalities of other devices.
package cryptotest

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/types"
	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// ServiceType of the Cryptotest service
const ServiceType = "cryptotest"

var expectedString map[id.RoomID]string

var helpMsgs = map[string]string{
	"crypto_help": ": Displays the help message",
	"crypto_challenge": "[prefix] : The bot sets a random challenge for the room and echoes it. " +
		"The client tested should respond with \"!crypto_response challenge\"." +
		"Alternatively the prefix that the challenge will be echoed with can be set.",
	"crypto_response":    "<challenge> : Should repeat the crypto_challenge's challenge code.",
	"crypto_new_session": ": Asks the bot to invalidate its current outgoing group session and create a new one.",
	"sas_verify_me":      "<device_id> : Asks the bot to start a decimal SAS verification transaction with the sender's specified device.",
	"sas_decimal_code": "<device_id> <sas1> <sas2> <sas3> : Sends the device's generated decimal SAS code for the bot to verify, " +
		"after a \"!sas_verify_me\" command.",
	"request_my_room_key": "<device_id> <sender_key> <session_id> : Asks the bot to request the room key for the current room " +
		"and given sender key and session ID from the sender's given device.",
	"forward_me_room_key": "<device_id> <sender_key> <session_id> : Asks the bot to send the room key for the current room " +
		"and given sender key and session ID to the sender's given device.",
}

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

func (s *Service) cmdCryptoHelp(roomID id.RoomID) (interface{}, error) {
	if s.inRoom(roomID) {
		helpTxt := "Supported crypto test methods:\n\n"
		for cmd, helpMsg := range helpMsgs {
			helpTxt += fmt.Sprintf("!%v %v\n\n", cmd, helpMsg)
		}
		return mevt.MessageEventContent{MsgType: mevt.MsgText, Body: helpTxt}, nil
	}
	return nil, nil
}

func (s *Service) cmdCryptoChallenge(roomID id.RoomID, arguments []string) (interface{}, error) {
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
}

func (s *Service) cmdCryptoResponse(userID id.UserID, roomID id.RoomID, arguments []string) (interface{}, error) {
	if s.inRoom(roomID) {
		if len(arguments) != 1 {
			return mevt.MessageEventContent{
				MsgType: mevt.MsgText,
				Body:    "!crypto_response " + helpMsgs["crypto_response"],
			}, nil
		}
		if arguments[0] == expectedString[roomID] {
			return mevt.MessageEventContent{
				MsgType: mevt.MsgText,
				Body:    fmt.Sprintf("Correct response received from %v", userID.String()),
			}, nil
		}
		return mevt.MessageEventContent{
			MsgType: mevt.MsgText,
			Body:    fmt.Sprintf("Incorrect response received from %v", userID.String()),
		}, nil
	}
	return nil, nil
}

func (s *Service) cmdCryptoNewSession(botClient *clients.BotClient, roomID id.RoomID) (interface{}, error) {
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
}

func (s *Service) cmdSASVerifyMe(botClient *clients.BotClient, roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
	if s.inRoom(roomID) {
		if len(arguments) != 1 {
			return mevt.MessageEventContent{
				MsgType: mevt.MsgText,
				Body:    "sas_verify_me " + helpMsgs["sas_verify_me"],
			}, nil
		}
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
}

func (s *Service) cmdSASVerifyDecimalCode(botClient *clients.BotClient, roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
	if s.inRoom(roomID) {
		if len(arguments) != 4 {
			return mevt.MessageEventContent{
				MsgType: mevt.MsgText,
				Body:    "sas_decimal_code " + helpMsgs["sas_decimal_code"],
			}, nil
		}
		deviceID := id.DeviceID(arguments[0])
		var decimalSAS crypto.DecimalSASData
		for i := 0; i < 3; i++ {
			sasCode, err := strconv.Atoi(arguments[i+1])
			if err != nil {
				log.WithFields(log.Fields{"user_id": userID, "device_id": deviceID}).WithError(err).Error("Error reading SAS code")
				return mevt.MessageEventContent{
					MsgType: mevt.MsgText,
					Body:    fmt.Sprintf("Error reading SAS code: %v", err),
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
}

func (s *Service) cmdRequestRoomKey(botClient *clients.BotClient, roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
	if s.inRoom(roomID) {
		if len(arguments) != 3 {
			return mevt.MessageEventContent{
				MsgType: mevt.MsgText,
				Body:    "request_my_room_key " + helpMsgs["request_my_room_key"],
			}, nil
		}
		deviceID := id.DeviceID(arguments[0])
		senderKey := id.SenderKey(arguments[1])
		sessionID := id.SessionID(arguments[2])
		receivedChan, err := botClient.SendRoomKeyRequest(userID, deviceID, roomID, senderKey, sessionID, time.Minute)
		if err != nil {
			log.WithFields(log.Fields{
				"user_id":    userID,
				"device_id":  deviceID,
				"sender_key": senderKey,
				"session_id": sessionID,
			}).WithError(err).Error("Error requesting room key")
			return mevt.MessageEventContent{
				MsgType: mevt.MsgText,
				Body:    fmt.Sprintf("Error requesting room key for session %v: %v", sessionID, err),
			}, nil
		}
		go func() {
			var result string
			received := <-receivedChan
			if received {
				result = "Key received successfully!"
			} else {
				result = "Key was not received in the time limit"
			}
			content := mevt.MessageEventContent{
				MsgType: mevt.MsgText,
				Body:    fmt.Sprintf("Room key request for session %v result: %v", sessionID, result),
			}
			if _, err := botClient.SendMessageEvent(roomID, mevt.EventMessage, content); err != nil {
				log.WithFields(log.Fields{
					"room_id": roomID,
					"content": content,
				}).WithError(err).Error("Failed to send room key request result to room")
			}
		}()
		return mevt.MessageEventContent{
			MsgType: mevt.MsgText,
			Body:    fmt.Sprintf("Sent room key request for session %v to device %v", sessionID, deviceID),
		}, nil
	}
	return nil, nil
}

func (s *Service) cmdForwardRoomKey(botClient *clients.BotClient, roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
	if s.inRoom(roomID) {
		if len(arguments) != 3 {
			return mevt.MessageEventContent{
				MsgType: mevt.MsgText,
				Body:    "forward_me_room_key  " + helpMsgs["forward_me_room_key"],
			}, nil
		}
		deviceID := id.DeviceID(arguments[0])
		senderKey := id.SenderKey(arguments[1])
		sessionID := id.SessionID(arguments[2])
		err := botClient.ForwardRoomKeyToDevice(userID, deviceID, roomID, senderKey, sessionID)
		if err != nil {
			log.WithFields(log.Fields{
				"user_id":    userID,
				"device_id":  deviceID,
				"sender_key": senderKey,
				"session_id": sessionID,
			}).WithError(err).Error("Error forwarding room key")
			return mevt.MessageEventContent{
				MsgType: mevt.MsgText,
				Body:    fmt.Sprintf("Error forwarding room key for session %v: %v", sessionID, err),
			}, nil
		}
		return mevt.MessageEventContent{
			MsgType: mevt.MsgText,
			Body:    fmt.Sprintf("Forwarded room key for session %v to device %v", sessionID, deviceID),
		}, nil
	}
	return nil, nil
}

// Commands supported:
//    !crypto_help  		Displays a help string
//    !crypto_challenge 	Sets a challenge for a room which clients should reply to with !crypto_response
//    !crypto_response		Used by the client to repeat the room challenge
//    !crypto_new_session 	Invalidates the bot's current outgoing session
// 	  !sas_verify_me 		Asks the bot to verify the sender
//    !sas_decimal_code		Sends the sender's SAS code to the bot for verification
//    !request_my_room_key	Asks the bot to request a room key from the sender
//    !forward_me_room_key	Asks the bot to forward a room key to the sender
// This service can be used for testing other clients by writing the commands above in a room where this service is enabled.
func (s *Service) Commands(cli types.MatrixClient) []types.Command {
	botClient := cli.(*clients.BotClient)
	return []types.Command{
		{
			Path: []string{"crypto_help"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				return s.cmdCryptoHelp(roomID)
			},
		},
		{
			Path: []string{"crypto_challenge"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				return s.cmdCryptoChallenge(roomID, arguments)
			},
		},
		{
			Path: []string{"crypto_response"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				return s.cmdCryptoResponse(userID, roomID, arguments)
			},
		},
		{
			Path: []string{"crypto_new_session"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				return s.cmdCryptoNewSession(botClient, roomID)
			},
		},
		{
			Path: []string{"sas_verify_me"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				return s.cmdSASVerifyMe(botClient, roomID, userID, arguments)
			},
		},
		{
			Path: []string{"sas_decimal_code"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				return s.cmdSASVerifyDecimalCode(botClient, roomID, userID, arguments)
			},
		},
		{
			Path: []string{"request_my_room_key"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				return s.cmdRequestRoomKey(botClient, roomID, userID, arguments)
			},
		},
		{
			Path: []string{"forward_me_room_key"},
			Command: func(roomID id.RoomID, userID id.UserID, arguments []string) (interface{}, error) {
				return s.cmdForwardRoomKey(botClient, roomID, userID, arguments)
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
