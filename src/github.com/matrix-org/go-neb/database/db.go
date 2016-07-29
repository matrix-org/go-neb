package database

import (
	"database/sql"
	"github.com/matrix-org/go-neb/matrix"
	"sort"
	"time"
)

// A ServiceDB stores the configuration for the services
type ServiceDB struct {
	db *sql.DB
}

// Open a SQL database to use as a ServiceDB. This will automatically create
// the necessary database tables if they aren't already present.
func Open(databaseType, databaseURL string) (serviceDB *ServiceDB, err error) {
	db, err := sql.Open(databaseType, databaseURL)
	if err != nil {
		return
	}
	if _, err = db.Exec(schemaSQL); err != nil {
		return
	}
	serviceDB = &ServiceDB{db: db}
	return
}

// StoreMatrixClientConfig stores the Matrix client config for a bot service.
// If a config already exists then it will be updated, otherwise a new config
// will be inserted. The previous config is returned.
func (d *ServiceDB) StoreMatrixClientConfig(config ClientConfig) (oldConfig ClientConfig, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		oldConfig, err = selectMatrixClientConfigTxn(txn, config.UserID)
		now := time.Now()
		if err == nil {
			return updateMatrixClientConfigTxn(txn, now, config)
		} else if err == sql.ErrNoRows {
			return insertMatrixClientConfigTxn(txn, now, config)
		} else {
			return err
		}
	})
	return
}

// LoadServiceUserIds loads the user ids used by the bots in the database and
// the rooms those bots should be joined to.
func (d *ServiceDB) LoadServiceUserIds() (userIDsToRooms map[string][]string, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		userIDsToRooms, err = selectServiceUserIDsTxn(txn)
		return err
	})
	return
}

// LoadMatrixClientConfig loads a Matrix client config from the database.
// Returns sql.ErrNoRows if the client isn't in the database.
func (d *ServiceDB) LoadMatrixClientConfig(userID string) (config ClientConfig, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		config, err = selectMatrixClientConfigTxn(txn, userID)
		return err
	})
	return
}

// LoadService loads a service from the database.
// Returns sql.ErrNoRows if the service isn't in the database.
func (d *ServiceDB) LoadService(serviceID string) (service Service, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		service, err = selectServiceTxn(txn, serviceID)
		return err
	})
	return
}

// LoadServicesInRoom loads all the bot services configured for a room.
// Returns the empty list if there aren't any services configured.
func (d *ServiceDB) LoadServicesInRoom(serviceUserID, roomID string) (services []Service, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		serviceIDs, err := selectRoomServicesTxn(txn, serviceUserID, roomID)
		if err != nil {
			return err
		}
		for _, serviceID := range serviceIDs {
			service, err := selectServiceTxn(txn, serviceID)
			if err != nil {
				return err
			}
			services = append(services, service)
		}
		return nil
	})
	return
}

// StoreService stores a service into the database either by inserting a new
// service or updating an existing service. Returns the old service if there
// was one.
func (d *ServiceDB) StoreService(service Service, client *matrix.Client) (oldService Service, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		oldService, err = selectServiceTxn(txn, service.ServiceID())
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		now := time.Now()

		var newRoomIDs []string
		var oldRoomIDs []string

		if oldService == nil {
			if err := insertServiceTxn(txn, now, service); err != nil {
				return err
			}
			newRoomIDs = service.RoomIDs()
		} else {
			if err := updateServiceTxn(txn, now, service); err != nil {
				return err
			}
			if service.ServiceUserID() == oldService.ServiceUserID() {
				oldRoomIDs, newRoomIDs = difference(
					oldService.RoomIDs(), service.RoomIDs(),
				)
			} else {
				oldRoomIDs = oldService.RoomIDs()
				newRoomIDs = service.RoomIDs()
			}
		}

		for _, roomID := range oldRoomIDs {
			if err := deleteRoomServiceTxn(
				txn, oldService.ServiceUserID(), roomID, service.ServiceID(),
			); err != nil {
				return err
			}
			// TODO: Leave the old rooms.
		}

		for _, roomID := range newRoomIDs {
			if err := insertRoomServiceTxn(
				txn, now, service.ServiceUserID(), roomID, service.ServiceID(),
			); err != nil {
				return err
			}

			// TODO: Making HTTP requests inside the database transaction is unfortunate.
			// But it is the easiest way of making sure that the changes we
			// made to the database get rolled back if the requests fail.
			if _, err := client.JoinRoom(roomID, ""); err != nil {
				// TODO: What happens to the rooms that we successfully joined?
				// Should we leave them now?
				return err
			}
		}

		return nil
	})
	return
}

func runTransaction(db *sql.DB, fn func(txn *sql.Tx) error) (err error) {
	txn, err := db.Begin()
	if err != nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			txn.Rollback()
			panic(r)
		} else if err != nil {
			txn.Rollback()
		} else {
			err = txn.Commit()
		}
	}()
	err = fn(txn)
	return
}

// difference returns the elements that are only in the first list and
// the elements that are only in the second. As a side-effect this sorts
// the input lists in-place.
func difference(a, b []string) (onlyA, onlyB []string) {
	sort.Strings(a)
	sort.Strings(b)
	for {
		if len(b) == 0 {
			onlyA = append(onlyA, a...)
			return
		}
		if len(a) == 0 {
			onlyB = append(onlyB, b...)
			return
		}
		xA := a[0]
		xB := b[0]
		if xA < xB {
			onlyA = append(onlyA, xA)
			a = a[1:]
		} else if xA > xB {
			onlyB = append(onlyB, xB)
			b = b[1:]
		} else {
			a = a[1:]
			b = b[1:]
		}
	}
}
