package clients

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/metrics"
	"github.com/matrix-org/go-neb/types"
	shellwords "github.com/mattn/go-shellwords"
	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// A Clients is a collection of clients used for bot services.
type Clients struct {
	db         database.Storer
	httpClient *http.Client
	dbMutex    sync.Mutex
	mapMutex   sync.Mutex
	clients    map[id.UserID]clientEntry
}

// New makes a new collection of matrix clients
func New(db database.Storer, cli *http.Client) *Clients {
	clients := &Clients{
		db:         db,
		httpClient: cli,
		clients:    make(map[id.UserID]clientEntry), // user_id => clientEntry
	}
	return clients
}

// Client gets a client for the userID
func (c *Clients) Client(userID id.UserID) (*mautrix.Client, error) {
	entry := c.getClient(userID)
	if entry.client != nil {
		return entry.client, nil
	}
	entry, err := c.loadClientFromDB(userID)
	return entry.client, err
}

// Update updates the config for a matrix client
func (c *Clients) Update(config api.ClientConfig) (api.ClientConfig, error) {
	_, old, err := c.updateClientInDB(config)
	return old.config, err
}

// Start listening on client /sync streams
func (c *Clients) Start() error {
	configs, err := c.db.LoadMatrixClientConfigs()
	if err != nil {
		return err
	}
	for _, cfg := range configs {
		if cfg.Sync {
			if _, err := c.Client(cfg.UserID); err != nil {
				return err
			}
		}
	}
	return nil
}

type clientEntry struct {
	config     api.ClientConfig
	client     *mautrix.Client
	olmMachine *crypto.OlmMachine
}

func (c *Clients) getClient(userID id.UserID) clientEntry {
	c.mapMutex.Lock()
	defer c.mapMutex.Unlock()
	return c.clients[userID]
}

func (c *Clients) setClient(client clientEntry) {
	c.mapMutex.Lock()
	defer c.mapMutex.Unlock()
	c.clients[client.config.UserID] = client
}

func (c *Clients) loadClientFromDB(userID id.UserID) (entry clientEntry, err error) {
	c.dbMutex.Lock()
	defer c.dbMutex.Unlock()

	entry = c.getClient(userID)
	if entry.client != nil {
		return
	}

	if entry.config, err = c.db.LoadMatrixClientConfig(userID); err != nil {
		if err == sql.ErrNoRows {
			err = fmt.Errorf("client with user ID %s does not exist", userID)
		}
		return
	}

	if err = c.initClient(&entry); err != nil {
		return
	}

	c.setClient(entry)
	return
}

func (c *Clients) updateClientInDB(newConfig api.ClientConfig) (new clientEntry, old clientEntry, err error) {
	c.dbMutex.Lock()
	defer c.dbMutex.Unlock()

	old = c.getClient(newConfig.UserID)
	if old.client != nil && old.config == newConfig {
		// Already have a client with that config.
		new = old
		return
	}

	new.config = newConfig

	if err = c.initClient(&new); err != nil {
		return
	}

	// set the new display name if they differ
	if old.config.DisplayName != new.config.DisplayName {
		if err := new.client.SetDisplayName(new.config.DisplayName); err != nil {
			// whine about it but don't stop: this isn't fatal.
			log.WithFields(log.Fields{
				log.ErrorKey:  err,
				"displayname": new.config.DisplayName,
				"user_id":     new.config.UserID,
			}).Error("Failed to set display name")
		}
	}

	if old.config, err = c.db.StoreMatrixClientConfig(new.config); err != nil {
		new.client.StopSync()
		return
	}

	if old.client != nil {
		old.client.StopSync()
		return
	}

	c.setClient(new)
	return
}

func (c *Clients) onMessageEvent(client *mautrix.Client, event *mevt.Event) {
	services, err := c.db.LoadServicesForUser(client.UserID)
	if err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey:      err,
			"room_id":         event.RoomID,
			"service_user_id": client.UserID,
		}).Warn("Error loading services")
	}

	if err := event.Content.ParseRaw(mevt.EventMessage); err != nil {
		return
	}

	message := event.Content.AsMessage()
	body := message.Body

	if body == "" {
		return
	}

	// filter m.notice to prevent loops
	if message.MsgType == mevt.MsgNotice {
		return
	}

	// replace all smart quotes with their normal counterparts so shellwords can parse it
	body = strings.Replace(body, `‘`, `'`, -1)
	body = strings.Replace(body, `’`, `'`, -1)
	body = strings.Replace(body, `“`, `"`, -1)
	body = strings.Replace(body, `”`, `"`, -1)

	var responses []interface{}

	for _, service := range services {
		if body[0] == '!' { // message is a command
			args, err := shellwords.Parse(body[1:])
			if err != nil {
				args = strings.Split(body[1:], " ")
			}

			if response := runCommandForService(service.Commands(client), event, args); response != nil {
				responses = append(responses, response)
			}
		} else { // message isn't a command, it might need expanding
			expansions := runExpansionsForService(service.Expansions(client), event, body)
			responses = append(responses, expansions...)
		}
	}

	for _, content := range responses {
		evtType := mevt.EventMessage
		curClient := c.clients[client.UserID]
		olmMachine := curClient.olmMachine
		if olmMachine.StateStore.IsEncrypted(event.RoomID) {
			fmt.Println(event.RoomID, "is enc")
			if sess, err := olmMachine.CryptoStore.GetOutboundGroupSession(event.RoomID); err != nil {
				fmt.Println("Error getting outbound", err)
			} else if sess == nil {
				if membs, err := client.JoinedMembers(event.RoomID); err != nil {
					fmt.Println(err)
				} else {
					memberIDs := make([]id.UserID, 0, len(membs.Joined))
					for member := range membs.Joined {
						memberIDs = append(memberIDs, member)
					}
					if err = olmMachine.ShareGroupSession(event.RoomID, memberIDs); err != nil {
						fmt.Println(err)
					}
				}
			}
			msgContent := mevt.Content{Parsed: content}
			if enc, err := olmMachine.EncryptMegolmEvent(event.RoomID, mevt.EventMessage, msgContent); err != nil {
				fmt.Println("error encoding", err)
			} else {
				content = enc
				evtType = mevt.EventEncrypted
			}
		}
		if _, err := client.SendMessageEvent(event.RoomID, evtType, content); err != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: err,
				"room_id":    event.RoomID,
				"user_id":    event.Sender,
				"content":    content,
			}).Print("Failed to send command response")
		}
	}
}

// runCommandForService runs a single command read from a matrix event. Runs
// the matching command with the longest path. Returns the JSON encodable
// content of a single matrix message event to use as a response or nil if no
// response is appropriate.
func runCommandForService(cmds []types.Command, event *mevt.Event, arguments []string) interface{} {
	var bestMatch *types.Command
	for i, command := range cmds {
		matches := command.Matches(arguments)
		betterMatch := bestMatch == nil || len(bestMatch.Path) < len(command.Path)
		if matches && betterMatch {
			bestMatch = &cmds[i]
		}
	}

	if bestMatch == nil {
		return nil
	}

	cmdArgs := arguments[len(bestMatch.Path):]
	log.WithFields(log.Fields{
		"room_id": event.RoomID,
		"user_id": event.Sender,
		"command": bestMatch.Path,
	}).Info("Executing command")
	content, err := bestMatch.Command(event.RoomID, event.Sender, cmdArgs)
	if err != nil {
		if content != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: err,
				"room_id":    event.RoomID,
				"user_id":    event.Sender,
				"command":    bestMatch.Path,
				"args":       cmdArgs,
			}).Warn("Command returned both error and content.")
		}
		metrics.IncrementCommand(bestMatch.Path[0], metrics.StatusFailure)
		content = mevt.MessageEventContent{
			MsgType: mevt.MsgNotice,
			Body:    err.Error(),
		}
	} else {
		metrics.IncrementCommand(bestMatch.Path[0], metrics.StatusSuccess)
	}

	return content
}

// run the expansions for a matrix event.
func runExpansionsForService(expans []types.Expansion, event *mevt.Event, body string) []interface{} {
	var responses []interface{}

	for _, expansion := range expans {
		matches := map[string]bool{}
		for _, matchingGroups := range expansion.Regexp.FindAllStringSubmatch(body, -1) {
			matchingText := matchingGroups[0] // first element is always the complete match
			if matches[matchingText] {
				// Only expand the first occurance of a matching string
				continue
			}
			matches[matchingText] = true
			if response := expansion.Expand(event.RoomID, event.Sender, matchingGroups); response != nil {
				responses = append(responses, response)
			}
		}
	}

	return responses
}

func (c *Clients) onBotOptionsEvent(client *mautrix.Client, event *mevt.Event) {
	// see if these options are for us. The state key is the user ID with a leading _
	// to get around restrictions in the HS about having user IDs as state keys.
	if event.StateKey == nil {
		return
	}
	targetUserID := id.UserID(strings.TrimPrefix(*event.StateKey, "_"))
	if targetUserID != client.UserID {
		return
	}
	// these options fully clobber what was there previously.
	opts := types.BotOptions{
		UserID:      client.UserID,
		RoomID:      event.RoomID,
		SetByUserID: event.Sender,
		Options:     event.Content.Raw,
	}
	if _, err := c.db.StoreBotOptions(opts); err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey:     err,
			"room_id":        event.RoomID,
			"bot_user_id":    client.UserID,
			"set_by_user_id": event.Sender,
		}).Error("Failed to persist bot options")
	}
}

func (c *Clients) onRoomMemberEvent(client *mautrix.Client, event *mevt.Event) {
	if err := event.Content.ParseRaw(mevt.StateMember); err != nil {
		return
	}
	if event.StateKey == nil || *event.StateKey != client.UserID.String() {
		return // not our member event
	}
	membership := event.Content.AsMember().Membership
	if membership == "invite" {
		logger := log.WithFields(log.Fields{
			"room_id":         event.RoomID,
			"service_user_id": client.UserID,
			"inviter":         event.Sender,
		})
		logger.Print("Accepting invite from user")

		content := struct {
			Inviter id.UserID `json:"inviter"`
		}{event.Sender}

		if _, err := client.JoinRoom(event.RoomID.String(), "", content); err != nil {
			logger.WithError(err).Print("Failed to join room")
		} else {
			logger.Print("Joined room")
		}
	}
}

func (c *Clients) initClient(clientEntry *clientEntry) error {
	config := clientEntry.config
	client, err := mautrix.NewClient(config.HomeserverURL, config.UserID, config.AccessToken)
	if err != nil {
		return err
	}

	client.Client = c.httpClient
	client.DeviceID = config.DeviceID
	syncer := client.Syncer.(*mautrix.DefaultSyncer)

	nebStore := &matrix.NEBStore{
		InMemoryStore: *mautrix.NewInMemoryStore(),
		Database:      c.db,
		ClientConfig:  config,
	}
	client.Store = nebStore

	// TODO: Check that the access token is valid for the userID by peforming
	// a request against the server.

	syncer.OnEventType(mevt.EventMessage, func(_ mautrix.EventSource, event *mevt.Event) {
		c.onMessageEvent(client, event)
	})

	syncer.OnEventType(mevt.Type{Type: "m.room.bot.options", Class: mevt.UnknownEventType}, func(_ mautrix.EventSource, event *mevt.Event) {
		c.onBotOptionsEvent(client, event)
	})

	if config.AutoJoinRooms {
		syncer.OnEventType(mevt.StateMember, func(_ mautrix.EventSource, event *mevt.Event) {
			c.onRoomMemberEvent(client, event)
		})
	}

	log.WithFields(log.Fields{
		"user_id":         config.UserID,
		"sync":            config.Sync,
		"auto_join_rooms": config.AutoJoinRooms,
		"since":           nebStore.LoadNextBatch(config.UserID),
	}).Info("Created new client")

	if config.Sync {
		go func() {
			for {
				if e := client.Sync(); e != nil {
					log.WithFields(log.Fields{
						log.ErrorKey: e,
						"user_id":    config.UserID,
					}).Error("Fatal Sync() error")
					time.Sleep(10 * time.Second)
				} else {
					log.WithField("user_id", config.UserID).Info("Stopping Sync()")
					return
				}
			}
		}()
	}

	clientEntry.client = client

	gobStore, err := crypto.NewGobStore("crypto.gob")
	if err != nil {
		return err
	}

	stateStore := StateStore{&nebStore.InMemoryStore}
	olmMachine := crypto.NewOlmMachine(client, CryptoMachineLogger{}, gobStore, &stateStore)
	olmMachine.Load()
	clientEntry.olmMachine = olmMachine

	syncer.OnSync(stateStore.UpdateStateStore)
	// Process sync response with olm machine
	syncer.OnSync(func(resp *mautrix.RespSync, since string) bool {
		olmMachine.ProcessSyncResponse(resp, since)
		if err := olmMachine.CryptoStore.Flush(); err != nil {
			fmt.Println("cryptostore flush err", err)
		}
		return true
	})

	syncer.OnEventType(mevt.StateMember, func(_ mautrix.EventSource, evt *mevt.Event) {
		olmMachine.HandleMemberEvent(evt)
	})

	syncer.OnEventType(mevt.EventEncrypted, func(source mautrix.EventSource, evt *mevt.Event) {
		evt.Content.ParseRaw(mevt.EventEncrypted)
		evt, err := olmMachine.DecryptMegolmEvent(evt)
		if err != nil {
			fmt.Println("decryption err", err)
		} else {
			if evt.Type == mevt.EventMessage {
				err = evt.Content.ParseRaw(mevt.EventMessage)
				if err != nil {
					fmt.Println("parsing msg err", err)
				} else {
					c.onMessageEvent(client, evt)
				}
			}
			fmt.Println("decrypted type", evt.Type)
		}
	})

	// Ignore events before neb's join event.
	eventIgnorer := mautrix.OldEventIgnorer{UserID: config.UserID}
	eventIgnorer.Register(syncer)

	return nil
}
