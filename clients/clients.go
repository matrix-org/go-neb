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
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// A Clients is a collection of clients used for bot services.
type Clients struct {
	db         database.Storer
	httpClient *http.Client
	dbMutex    sync.Mutex
	mapMutex   sync.Mutex
	clients    map[id.UserID]BotClient
}

// New makes a new collection of matrix clients
func New(db database.Storer, cli *http.Client) *Clients {
	clients := &Clients{
		db:         db,
		httpClient: cli,
		clients:    make(map[id.UserID]BotClient), // user_id => BotClient
	}
	return clients
}

// Client gets a client for the userID
func (c *Clients) Client(userID id.UserID) (*BotClient, error) {
	entry := c.getClient(userID)
	if entry.Client != nil {
		return &entry, nil
	}
	entry, err := c.loadClientFromDB(userID)
	return &entry, err
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

func (c *Clients) getClient(userID id.UserID) BotClient {
	c.mapMutex.Lock()
	defer c.mapMutex.Unlock()
	return c.clients[userID]
}

func (c *Clients) setClient(client BotClient) {
	c.mapMutex.Lock()
	defer c.mapMutex.Unlock()
	c.clients[client.config.UserID] = client
}

func (c *Clients) loadClientFromDB(userID id.UserID) (entry BotClient, err error) {
	c.dbMutex.Lock()
	defer c.dbMutex.Unlock()

	entry = c.getClient(userID)
	if entry.Client != nil {
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

func (c *Clients) updateClientInDB(newConfig api.ClientConfig) (new, old BotClient, err error) {
	c.dbMutex.Lock()
	defer c.dbMutex.Unlock()

	old = c.getClient(newConfig.UserID)
	if old.Client != nil && old.config == newConfig {
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
		if err := new.SetDisplayName(new.config.DisplayName); err != nil {
			// whine about it but don't stop: this isn't fatal.
			log.WithFields(log.Fields{
				log.ErrorKey:  err,
				"displayname": new.config.DisplayName,
				"user_id":     new.config.UserID,
			}).Error("Failed to set display name")
		}
	}

	if old.config, err = c.db.StoreMatrixClientConfig(new.config); err != nil {
		new.StopSync()
		return
	}

	if old.Client != nil {
		old.Client.StopSync()
		return
	}

	c.setClient(new)
	return
}

func (c *Clients) onMessageEvent(botClient *BotClient, event *mevt.Event) {
	services, err := c.db.LoadServicesForUser(botClient.UserID)
	if err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey:      err,
			"room_id":         event.RoomID,
			"service_user_id": botClient.UserID,
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

			if response := runCommandForService(service.Commands(botClient), event, args); response != nil {
				responses = append(responses, response)
			}
		} else { // message isn't a command, it might need expanding
			expansions := runExpansionsForService(service.Expansions(botClient), event, body)
			responses = append(responses, expansions...)
		}
	}

	for _, content := range responses {
		if _, err := botClient.SendMessageEvent(event.RoomID, mevt.EventMessage, content); err != nil {
			log.WithFields(log.Fields{
				"room_id": event.RoomID,
				"content": content,
				"sender":  event.Sender,
			}).WithError(err).Error("Failed to send command response")
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

func (c *Clients) initClient(botClient *BotClient) error {
	config := botClient.config
	client, err := mautrix.NewClient(config.HomeserverURL, config.UserID, config.AccessToken)
	if err != nil {
		return err
	}

	client.Client = c.httpClient
	client.DeviceID = config.DeviceID
	botClient.Client = client

	syncer := client.Syncer.(*mautrix.DefaultSyncer)

	nebStore := &matrix.NEBStore{
		InMemoryStore: *mautrix.NewInMemoryStore(),
		Database:      c.db,
		ClientConfig:  config,
	}
	client.Store = nebStore

	// TODO: Check that the access token is valid for the userID by peforming
	// a request against the server.

	if err = botClient.InitOlmMachine(client, nebStore, c.db); err != nil {
		return err
	}

	botClient.Register(syncer)

	syncer.OnEventType(mevt.EventMessage, func(_ mautrix.EventSource, event *mevt.Event) {
		c.onMessageEvent(botClient, event)
	})

	syncer.OnEventType(mevt.Type{Type: "m.room.bot.options", Class: mevt.UnknownEventType}, func(_ mautrix.EventSource, event *mevt.Event) {
		c.onBotOptionsEvent(botClient.Client, event)
	})

	if config.AutoJoinRooms {
		syncer.OnEventType(mevt.StateMember, func(_ mautrix.EventSource, event *mevt.Event) {
			c.onRoomMemberEvent(client, event)
		})
	}

	// When receiving an encrypted event, attempt to decrypt it using the BotClient's capabilities.
	// If successfully decrypted propagate the decrypted event to the clients.
	syncer.OnEventType(mevt.EventEncrypted, func(source mautrix.EventSource, evt *mevt.Event) {
		if err := evt.Content.ParseRaw(mevt.EventEncrypted); err != nil {
			log.WithError(err).Error("Failed to parse encrypted message")
			return
		}
		encContent := evt.Content.AsEncrypted()
		decrypted, err := botClient.DecryptMegolmEvent(evt)
		if err != nil {
			log.WithFields(log.Fields{
				"user_id":    config.UserID,
				"device_id":  encContent.DeviceID,
				"session_id": encContent.SessionID,
				"sender_key": encContent.SenderKey,
			}).WithError(err).Error("Failed to decrypt message")
		} else {
			if decrypted.Type == mevt.EventMessage {
				err = decrypted.Content.ParseRaw(mevt.EventMessage)
				if err != nil {
					log.WithError(err).Error("Could not parse decrypted message event")
				} else {
					c.onMessageEvent(botClient, decrypted)
				}
			}
			log.WithFields(log.Fields{
				"type":      evt.Type,
				"sender":    evt.Sender,
				"room_id":   evt.RoomID,
				"state_key": evt.StateKey,
			}).Trace("Decrypted event successfully")
		}
	})

	// Ignore events before neb's join event.
	eventIgnorer := mautrix.OldEventIgnorer{UserID: config.UserID}
	eventIgnorer.Register(syncer)

	log.WithFields(log.Fields{
		"user_id":         config.UserID,
		"device_id":       config.DeviceID,
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

	return nil
}
