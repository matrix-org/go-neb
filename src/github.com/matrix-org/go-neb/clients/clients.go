package clients

import (
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"net/url"
	"sync"
)

// A Clients is a collection of clients used for bot services.
type Clients struct {
	db       *database.ServiceDB
	dbMutex  sync.Mutex
	mapMutex sync.Mutex
	clients  map[string]clientEntry
}

// Make a new collection of matrix clients
func Make(db *database.ServiceDB) *Clients {
	clients := &Clients{
		db:      db,
		clients: make(map[string]clientEntry),
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
func (c *Clients) Update(config database.ClientConfig) (database.ClientConfig, error) {
	_, old, err := c.updateClientInDB(config)
	return old.config, err
}

// Start the clients in the database and join them to the rooms.
func (c *Clients) Start() error {
	userIDsToRooms, err := c.db.LoadServiceUserIds()
	if err != nil {
		return err
	}
	for userID, roomIDs := range userIDsToRooms {
		client, err := c.Client(userID)
		if err != nil {
			log.WithFields(log.Fields{
				log.ErrorKey:      err,
				"service_user_id": userID,
			}).Warn("Error loading matrix client")
			return err
		}
		for _, roomID := range roomIDs {
			_, err := client.JoinRoom(roomID, "")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type clientEntry struct {
	config database.ClientConfig
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

func (c *Clients) updateClientInDB(newConfig database.ClientConfig) (new clientEntry, old clientEntry, err error) {
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

func (c *Clients) newClient(config database.ClientConfig) (*matrix.Client, error) {

	homeserverURL, err := url.Parse(config.HomeserverURL)
	if err != nil {
		return nil, err
	}

	client := matrix.NewClient(homeserverURL, config.AccessToken, config.UserID)

	// TODO: Check that the access token is valid for the userID by peforming
	// a request against the server.

	client.Worker.OnEventType("m.room.message", func(event *matrix.Event) {
		services, err := c.db.LoadServicesInRoom(client.UserID, event.RoomID)
		if err != nil {
			log.WithFields(log.Fields{
				log.ErrorKey:      err,
				"room_id":         event.RoomID,
				"service_user_id": client.UserID,
			}).Warn("Error loading services")
		}
		var plugins []plugin.Plugin
		for _, service := range services {
			plugins = append(plugins, service.Plugin(event.RoomID))
		}
		plugin.OnMessage(plugins, client, event)
	})

	go client.Sync()

	return client, nil
}
