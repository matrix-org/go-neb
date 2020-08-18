package clients

import (
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/event"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// maximumVerifications is the number of maximum ongoing SAS verifications at a time.
// After this limit we start ignoring verification requests.
const maximumVerifications = 100

// BotClient represents one of the bot's sessions, with a specific User and Device ID.
// It can be used for sending messages and retrieving information about the rooms that
// the client has joined.
type BotClient struct {
	*mautrix.Client
	config                   api.ClientConfig
	olmMachine               *crypto.OlmMachine
	stateStore               *NebStateStore
	verificationSAS          *sync.Map
	ongoingVerificationCount int32
}

// InitOlmMachine initializes a BotClient's internal OlmMachine given a client object and a Neb store,
// which will be used to store room information.
func (botClient *BotClient) InitOlmMachine(client *mautrix.Client, nebStore *matrix.NEBStore) (err error) {

	var cryptoStore crypto.Store
	cryptoLogger := CryptoMachineLogger{}
	if sdb, ok := database.GetServiceDB().(*database.ServiceDB); ok {
		// Create an SQL crypto store based on the ServiceDB used
		db, dialect := sdb.GetSQLDb()
		accountID := botClient.config.UserID.String() + "-" + client.DeviceID.String()
		sqlCryptoStore := crypto.NewSQLCryptoStore(db, dialect, accountID, client.DeviceID, []byte(client.DeviceID.String()+"pickle"), cryptoLogger)
		// Try to create the tables if they are missing
		if err = sqlCryptoStore.CreateTables(); err != nil {
			return
		}
		cryptoStore = sqlCryptoStore
		cryptoLogger.Debug("Using SQL backend as the crypto store")
	} else {
		deviceID := client.DeviceID.String()
		if deviceID == "" {
			deviceID = "_empty_device_id"
		}
		cryptoStore, err = crypto.NewGobStore(deviceID + ".gob")
		if err != nil {
			return
		}
		cryptoLogger.Debug("Using gob storage as the crypto store")
	}

	botClient.stateStore = &NebStateStore{&nebStore.InMemoryStore}
	olmMachine := crypto.NewOlmMachine(client, cryptoLogger, cryptoStore, botClient.stateStore)

	regexes := make([]*regexp.Regexp, 0, len(botClient.config.AcceptVerificationFromUsers))
	for _, userRegex := range botClient.config.AcceptVerificationFromUsers {
		regex, err := regexp.Compile(userRegex)
		if err != nil {
			cryptoLogger.Error("Error compiling regex %v: %v", userRegex, err)
		} else {
			regexes = append(regexes, regex)
		}
	}
	olmMachine.AcceptVerificationFrom = func(_ string, otherDevice *crypto.DeviceIdentity) (crypto.VerificationRequestResponse, crypto.VerificationHooks) {
		for _, regex := range regexes {
			if regex.MatchString(otherDevice.UserID.String()) {
				if atomic.LoadInt32(&botClient.ongoingVerificationCount) >= maximumVerifications {
					cryptoLogger.Trace("User ID %v matches regex %v but we are currently at maximum verifications, ignoring...", otherDevice.UserID, regex)
					return crypto.IgnoreRequest, botClient
				}
				cryptoLogger.Trace("User ID %v matches regex %v, accepting SAS request", otherDevice.UserID, regex)
				atomic.AddInt32(&botClient.ongoingVerificationCount, 1)
				return crypto.AcceptRequest, botClient
			}
		}
		cryptoLogger.Trace("User ID %v does not match any regex, rejecting SAS request", otherDevice.UserID)
		return crypto.RejectRequest, botClient
	}
	if err = olmMachine.Load(); err != nil {
		return
	}
	botClient.olmMachine = olmMachine

	return nil
}

// Register registers a BotClient's Sync and StateMember event callbacks to update its internal state
// when new events arrive.
func (botClient *BotClient) Register(syncer mautrix.ExtensibleSyncer) {
	syncer.OnEventType(mevt.StateMember, func(_ mautrix.EventSource, evt *mevt.Event) {
		botClient.olmMachine.HandleMemberEvent(evt)
	})
	syncer.OnSync(botClient.syncCallback)
}

func (botClient *BotClient) syncCallback(resp *mautrix.RespSync, since string) bool {
	botClient.stateStore.UpdateStateStore(resp)
	botClient.olmMachine.ProcessSyncResponse(resp, since)
	if err := botClient.olmMachine.CryptoStore.Flush(); err != nil {
		log.WithError(err).Error("Could not flush crypto store")
	}
	return true
}

// DecryptMegolmEvent attempts to decrypt an incoming m.room.encrypted message using the session information
// already present in the OlmMachine. The corresponding decrypted event is then returned.
// If it fails, usually because the session is not known, an error is returned.
func (botClient *BotClient) DecryptMegolmEvent(evt *mevt.Event) (*mevt.Event, error) {
	return botClient.olmMachine.DecryptMegolmEvent(evt)
}

// SendMessageEvent sends the given content to the given room ID using this BotClient as a message event.
// If the target room has enabled encryption, a megolm session is created if one doesn't already exist
// and the message is sent after being encrypted.
func (botClient *BotClient) SendMessageEvent(roomID id.RoomID, evtType mevt.Type, content interface{},
	extra ...mautrix.ReqSendEvent) (*mautrix.RespSendEvent, error) {

	olmMachine := botClient.olmMachine
	if olmMachine.StateStore.IsEncrypted(roomID) {
		// Check if there is already a megolm session
		if sess, err := olmMachine.CryptoStore.GetOutboundGroupSession(roomID); err != nil {
			return nil, err
		} else if sess == nil || sess.Expired() || !sess.Shared {
			// No error but valid, shared session does not exist
			memberIDs, err := botClient.stateStore.GetJoinedMembers(roomID)
			if err != nil {
				return nil, err
			}
			// Share group session with room members
			if err = olmMachine.ShareGroupSession(roomID, memberIDs); err != nil {
				return nil, err
			}
		}
		enc, err := olmMachine.EncryptMegolmEvent(roomID, mevt.EventMessage, content)
		if err != nil {
			return nil, err
		}
		content = enc
		evtType = mevt.EventEncrypted
	}
	return botClient.Client.SendMessageEvent(roomID, evtType, content, extra...)
}

// Sync loops to keep syncing the client with the homeserver by calling the /sync endpoint.
func (botClient *BotClient) Sync() {
	// Get the state store up to date
	resp, err := botClient.SyncRequest(30000, "", "", true, mevt.PresenceOnline)
	if err != nil {
		log.WithError(err).Error("Error performing initial sync")
		return
	}
	botClient.stateStore.UpdateStateStore(resp)

	for {
		if e := botClient.Client.Sync(); e != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: e,
				"user_id":    botClient.config.UserID,
			}).Error("Fatal Sync() error")
			time.Sleep(10 * time.Second)
		} else {
			log.WithField("user_id", botClient.config.UserID).Info("Stopping Sync()")
			return
		}
	}
}

// VerifySASMatch returns whether the received SAS matches the SAS that the bot generated.
// It retrieves the SAS of the other device from the bot client's SAS sync map, where it was stored by the `SubmitDecimalSAS` function.
func (botClient *BotClient) VerifySASMatch(otherDevice *crypto.DeviceIdentity, sas crypto.SASData) bool {
	log.WithFields(log.Fields{
		"otherUser":   otherDevice.UserID,
		"otherDevice": otherDevice.DeviceID,
	}).Infof("Waiting for SAS")
	if sas.Type() != event.SASDecimal {
		log.Warnf("Unsupported SAS type: %v", sas.Type())
		return false
	}
	key := otherDevice.UserID.String() + ":" + otherDevice.DeviceID.String()
	sasChan, loaded := botClient.verificationSAS.LoadOrStore(key, make(chan crypto.DecimalSASData))
	if !loaded {
		// if we created the chan, delete it after the timeout duration
		defer botClient.verificationSAS.Delete(key)
	}
	select {
	case otherSAS := <-sasChan.(chan crypto.DecimalSASData):
		ourSAS := sas.(crypto.DecimalSASData)
		log.WithFields(log.Fields{
			"otherUser":   otherDevice.UserID,
			"otherDevice": otherDevice.DeviceID,
		}).Warnf("Our SAS: %v, Received SAS: %v, Match: %v", ourSAS, otherSAS, ourSAS == otherSAS)
		return ourSAS == otherSAS
	case <-time.After(botClient.olmMachine.DefaultSASTimeout):
		log.Warnf("Timed out while waiting for SAS from device %v", otherDevice.DeviceID)
	}
	return false
}

// SubmitDecimalSAS stores the received decimal SAS from another device to compare to the local one.
// It stores the SAS in the bot client's SAS sync map to be retrieved from the `VerifySASMatch` function.
func (botClient *BotClient) SubmitDecimalSAS(otherUser id.UserID, otherDevice id.DeviceID, sas crypto.DecimalSASData) {
	key := otherUser.String() + ":" + otherDevice.String()
	sasChan, loaded := botClient.verificationSAS.LoadOrStore(key, make(chan crypto.DecimalSASData))
	go func() {
		if !loaded {
			// if we created the chan, delete it after the timeout duration
			defer botClient.verificationSAS.Delete(key)
		}
		// insert to channel in goroutine to avoid blocking if we are not expecting a SAS for this user/device right now
		select {
		case sasChan.(chan crypto.DecimalSASData) <- crypto.DecimalSASData(sas):
		case <-time.After(botClient.olmMachine.DefaultSASTimeout):
			log.Warnf("Timed out while trying to send SAS for device %v", otherDevice)
		}
	}()
}

// VerificationMethods returns the supported SAS verification methods.
// As a bot we only support decimal as it's easier to understand.
func (botClient *BotClient) VerificationMethods() []crypto.VerificationMethod {
	return []crypto.VerificationMethod{
		crypto.VerificationMethodDecimal{},
	}
}

// OnCancel is called when a SAS verification is canceled.
func (botClient *BotClient) OnCancel(cancelledByUs bool, reason string, reasonCode event.VerificationCancelCode) {
	atomic.AddInt32(&botClient.ongoingVerificationCount, -1)
	log.Tracef("Verification cancelled with reason: %v", reason)
}

// OnSuccess is called when a SAS verification is successful.
func (botClient *BotClient) OnSuccess() {
	atomic.AddInt32(&botClient.ongoingVerificationCount, -1)
	log.Trace("Verification was successful")
}

// InvalidateRoomSession invalidates the outbound group session for the given room.
func (botClient *BotClient) InvalidateRoomSession(roomID id.RoomID) (id.SessionID, error) {
	outbound, err := botClient.olmMachine.CryptoStore.GetOutboundGroupSession(roomID)
	if err != nil {
		return "", err
	}
	if outbound == nil {
		return "", errors.New("No group session found for this room")
	}
	return outbound.ID(), botClient.olmMachine.CryptoStore.RemoveOutboundGroupSession(roomID)
}

// StartSASVerification starts a new SAS verification with the given user and device ID and returns the transaction ID if successful.
func (botClient *BotClient) StartSASVerification(userID id.UserID, deviceID id.DeviceID) (string, error) {
	device, err := botClient.olmMachine.GetOrFetchDevice(userID, deviceID)
	if err != nil {
		return "", err
	}
	return botClient.olmMachine.NewSimpleSASVerificationWith(device, botClient)
}

// SendRoomKeyRequest sends a room key request to another device.
func (botClient *BotClient) SendRoomKeyRequest(userID id.UserID, deviceID id.DeviceID, roomID id.RoomID,
	senderKey id.SenderKey, sessionID id.SessionID, timeout time.Duration) (chan bool, error) {

	ctx, _ := context.WithTimeout(context.Background(), timeout)
	return botClient.olmMachine.RequestRoomKey(ctx, userID, deviceID, roomID, senderKey, sessionID)
}

// ForwardRoomKeyToDevice sends a room key to another device.
func (botClient *BotClient) ForwardRoomKeyToDevice(userID id.UserID, deviceID id.DeviceID, roomID id.RoomID, senderKey id.SenderKey,
	sessionID id.SessionID) error {

	device, err := botClient.olmMachine.GetOrFetchDevice(userID, deviceID)
	if err != nil {
		return err
	}

	igs, err := botClient.olmMachine.CryptoStore.GetGroupSession(roomID, senderKey, sessionID)
	if err != nil {
		return err
	} else if igs == nil {
		return errors.New("Group session not found")
	}

	exportedKey, err := igs.Internal.Export(igs.Internal.FirstKnownIndex())
	if err != nil {
		return err
	}

	forwardedRoomKey := event.Content{
		Parsed: &event.ForwardedRoomKeyEventContent{
			RoomKeyEventContent: event.RoomKeyEventContent{
				Algorithm:  id.AlgorithmMegolmV1,
				RoomID:     igs.RoomID,
				SessionID:  igs.ID(),
				SessionKey: exportedKey,
			},
			SenderKey:          senderKey,
			ForwardingKeyChain: igs.ForwardingChains,
			SenderClaimedKey:   igs.SigningKey,
		},
	}

	return botClient.olmMachine.SendEncryptedToDevice(device, forwardedRoomKey)
}
