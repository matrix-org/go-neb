package clients

import (
	"net/http"
	"net/url"
	"strings"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/metrics"
	"github.com/matrix-org/go-neb/types"
	shellwords "github.com/mattn/go-shellwords"
)

type nextBatchStore struct {
	db *database.ServiceDB
}

func (s nextBatchStore) Save(userID, nextBatch string) {
	if err := s.db.UpdateNextBatch(userID, nextBatch); err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey: err,
			"user_id":    userID,
			"next_batch": nextBatch,
		}).Error("Failed to persist next_batch token")
	}
}
func (s nextBatchStore) Load(userID string) string {
	token, err := s.db.LoadNextBatch(userID)
	if err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey: err,
			"user_id":    userID,
		}).Error("Failed to load next_batch token")
		return ""
	}
	return token
}

// A Clients is a collection of clients used for bot services.
type Clients struct {
	db         *database.ServiceDB
	httpClient *http.Client
	dbMutex    sync.Mutex
	mapMutex   sync.Mutex
	clients    map[string]clientEntry
}

// New makes a new collection of matrix clients
func New(db *database.ServiceDB, cli *http.Client) *Clients {
	clients := &Clients{
		db:         db,
		httpClient: cli,
		clients:    make(map[string]clientEntry), // user_id => clientEntry
	}
	return clients
}

// Client gets a client for the userID
func (c *Clients) Client(userID string) (*matrix.Client, error) {
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
	config api.ClientConfig
	client *matrix.Client
}

func (c *Clients) getClient(userID string) clientEntry {
	c.mapMutex.Lock()
	defer c.mapMutex.Unlock()
	return c.clients[userID]
}

func (c *Clients) setClient(client clientEntry) {
	c.mapMutex.Lock()
	defer c.mapMutex.Unlock()
	c.clients[client.config.UserID] = client
}

func (c *Clients) loadClientFromDB(userID string) (entry clientEntry, err error) {
	c.dbMutex.Lock()
	defer c.dbMutex.Unlock()

	entry = c.getClient(userID)
	if entry.client != nil {
		return
	}

	if entry.config, err = c.db.LoadMatrixClientConfig(userID); err != nil {
		return
	}

	if entry.client, err = c.newClient(entry.config); err != nil {
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

	if new.client, err = c.newClient(new.config); err != nil {
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

func (c *Clients) onMessageEvent(client *matrix.Client, event *matrix.Event) {
	services, err := c.db.LoadServicesForUser(client.UserID)
	if err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey:      err,
			"room_id":         event.RoomID,
			"service_user_id": client.UserID,
		}).Warn("Error loading services")
	}

	body, ok := event.Body()
	if !ok || body == "" {
		return
	}

	// filter m.notice to prevent loops
	if msgtype, ok := event.MessageType(); !ok || msgtype == "m.notice" {
		return
	}

	var responses []interface{}

	for _, service := range services {
		if body[0] == '!' { // message is a command
			args, err := shellwords.Parse(body[1:])
			if err != nil {
				args = strings.Split(body[1:], " ")
			}

			if response := runCommandForService(service.Commands(client, event.RoomID), event, args); response != nil {
				responses = append(responses, response)
			}
		} else { // message isn't a command, it might need expanding
			expansions := runExpansionsForService(service.Expansions(client, event.RoomID), event, body)
			responses = append(responses, expansions...)
		}
	}

	for _, content := range responses {
		if _, err := client.SendMessageEvent(event.RoomID, "m.room.message", content); err != nil {
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
func runCommandForService(cmds []types.Command, event *matrix.Event, arguments []string) interface{} {
	var bestMatch *types.Command
	for _, command := range cmds {
		matches := command.Matches(arguments)
		betterMatch := bestMatch == nil || len(bestMatch.Path) < len(command.Path)
		if matches && betterMatch {
			bestMatch = &command
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
		content = matrix.TextMessage{"m.notice", err.Error()}
	} else {
		metrics.IncrementCommand(bestMatch.Path[0], metrics.StatusSuccess)
	}

	return content
}

// run the expansions for a matrix event.
func runExpansionsForService(expans []types.Expansion, event *matrix.Event, body string) []interface{} {
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

func (c *Clients) onBotOptionsEvent(client *matrix.Client, event *matrix.Event) {
	// see if these options are for us. The state key is the user ID with a leading _
	// to get around restrictions in the HS about having user IDs as state keys.
	targetUserID := strings.TrimPrefix(event.StateKey, "_")
	if targetUserID != client.UserID {
		return
	}
	// these options fully clobber what was there previously.
	opts := types.BotOptions{
		UserID:      client.UserID,
		RoomID:      event.RoomID,
		SetByUserID: event.Sender,
		Options:     event.Content,
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

func (c *Clients) onRoomMemberEvent(client *matrix.Client, event *matrix.Event) {
	if event.StateKey != client.UserID {
		return // not our member event
	}
	m := event.Content["membership"]
	membership, ok := m.(string)
	if !ok {
		return
	}
	if membership == "invite" {
		logger := log.WithFields(log.Fields{
			"room_id":         event.RoomID,
			"service_user_id": client.UserID,
			"inviter":         event.Sender,
		})
		logger.Print("Accepting invite from user")

		if _, err := client.JoinRoom(event.RoomID, "", event.Sender); err != nil {
			logger.WithError(err).Print("Failed to join room")
		} else {
			logger.Print("Joined room")
		}
	}
}

func (c *Clients) newClient(config api.ClientConfig) (*matrix.Client, error) {
	homeserverURL, err := url.Parse(config.HomeserverURL)
	if err != nil {
		return nil, err
	}

	client := matrix.NewClient(c.httpClient, homeserverURL, config.AccessToken, config.UserID)
	client.NextBatchStorer = nextBatchStore{c.db}

	// TODO: Check that the access token is valid for the userID by peforming
	// a request against the server.

	client.Worker.OnEventType("m.room.message", func(event *matrix.Event) {
		c.onMessageEvent(client, event)
	})

	client.Worker.OnEventType("m.room.bot.options", func(event *matrix.Event) {
		c.onBotOptionsEvent(client, event)
	})

	if config.AutoJoinRooms {
		client.Worker.OnEventType("m.room.member", func(event *matrix.Event) {
			c.onRoomMemberEvent(client, event)
		})
	}

	if config.Sync {
		go client.Sync()
	}

	return client, nil
}
