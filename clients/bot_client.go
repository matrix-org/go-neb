package clients

import (
	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// BotClient represents one of the bot's sessions, with a specific User and Device ID.
// It can be used for sending messages and retrieving information about the rooms that
// the client has joined.
type BotClient struct {
	*mautrix.Client
	config     api.ClientConfig
	olmMachine *crypto.OlmMachine
	stateStore *NebStateStore
}

// InitOlmMachine initializes a BotClient's internal OlmMachine given a client object and a Neb store,
// which will be used to store room information.
func (botClient *BotClient) InitOlmMachine(client *mautrix.Client, nebStore *matrix.NEBStore) (err error) {

	var cryptoStore crypto.Store
	cryptoLogger := CryptoMachineLogger{}
	if sdb, ok := database.GetServiceDB().(*database.ServiceDB); ok {
		// Create an SQL crypto store based on the ServiceDB used
		db, dialect := sdb.GetSQLDb()
		sqlCryptoStore := crypto.NewSQLCryptoStore(db, dialect, client.DeviceID, []byte(client.DeviceID.String()), cryptoLogger)
		// Try to create the tables if they are missing
		if err = sqlCryptoStore.CreateTables(); err != nil {
			return
		}
		cryptoStore = sqlCryptoStore
		cryptoLogger.Debug("Using SQL backend as the crypto store")
	} else {
		cryptoStore, err = crypto.NewGobStore(client.DeviceID.String() + ".gob")
		if err != nil {
			return
		}
		cryptoLogger.Debug("Using gob storage as the crypto store")
	}

	botClient.stateStore = &NebStateStore{&nebStore.InMemoryStore}
	olmMachine := crypto.NewOlmMachine(client, cryptoLogger, cryptoStore, botClient.stateStore)
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
