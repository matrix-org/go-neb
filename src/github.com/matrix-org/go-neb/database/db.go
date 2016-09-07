package database

import (
	"database/sql"
	"github.com/matrix-org/go-neb/types"
	"time"
)

// A ServiceDB stores the configuration for the services
type ServiceDB struct {
	db *sql.DB
}

// A single global instance of the service DB.
// XXX: I can't think of any way of doing this without one without creating
//      cyclical dependencies somewhere -- Kegan
var globalServiceDB *ServiceDB

// SetServiceDB sets the global service DB instance.
func SetServiceDB(db *ServiceDB) {
	globalServiceDB = db
}

// GetServiceDB gets the global service DB instance.
func GetServiceDB() *ServiceDB {
	return globalServiceDB
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
func (d *ServiceDB) StoreMatrixClientConfig(config types.ClientConfig) (oldConfig types.ClientConfig, err error) {
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

// LoadMatrixClientConfigs loads all Matrix client configs from the database.
func (d *ServiceDB) LoadMatrixClientConfigs() (configs []types.ClientConfig, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		configs, err = selectMatrixClientConfigsTxn(txn)
		return err
	})
	return
}

// LoadMatrixClientConfig loads a Matrix client config from the database.
// Returns sql.ErrNoRows if the client isn't in the database.
func (d *ServiceDB) LoadMatrixClientConfig(userID string) (config types.ClientConfig, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		config, err = selectMatrixClientConfigTxn(txn, userID)
		return err
	})
	return
}

// UpdateNextBatch updates the next_batch token for the given user.
func (d *ServiceDB) UpdateNextBatch(userID, nextBatch string) (err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		return updateNextBatchTxn(txn, userID, nextBatch)
	})
	return
}

// LoadNextBatch loads the next_batch token for the given user.
func (d *ServiceDB) LoadNextBatch(userID string) (nextBatch string, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		nextBatch, err = selectNextBatchTxn(txn, userID)
		return err
	})
	return
}

// LoadService loads a service from the database.
// Returns sql.ErrNoRows if the service isn't in the database.
func (d *ServiceDB) LoadService(serviceID string) (service types.Service, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		service, err = selectServiceTxn(txn, serviceID)
		return err
	})
	return
}

// DeleteService deletes the given service from the database.
func (d *ServiceDB) DeleteService(serviceID string) (err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		return deleteServiceTxn(txn, serviceID)
	})
	return
}

// LoadServicesForUser loads all the bot services configured for a given user.
// Returns an empty list if there aren't any services configured.
func (d *ServiceDB) LoadServicesForUser(serviceUserID string) (services []types.Service, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		services, err = selectServicesForUserTxn(txn, serviceUserID)
		if err != nil {
			return err
		}
		return nil
	})
	return
}

// StoreService stores a service into the database either by inserting a new
// service or updating an existing service. Returns the old service if there
// was one.
func (d *ServiceDB) StoreService(service types.Service) (oldService types.Service, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		oldService, err = selectServiceTxn(txn, service.ServiceID())
		if err == sql.ErrNoRows {
			return insertServiceTxn(txn, time.Now(), service)
		} else if err != nil {
			return err
		} else {
			return updateServiceTxn(txn, time.Now(), service)
		}
	})
	return
}

// LoadAuthRealm loads an AuthRealm from the database.
// Returns sql.ErrNoRows if the realm isn't in the database.
func (d *ServiceDB) LoadAuthRealm(realmID string) (realm types.AuthRealm, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		realm, err = selectRealmTxn(txn, realmID)
		return err
	})
	return
}

// LoadAuthRealmsByType loads all auth realms with the given type from the database.
// The realms are ordered based on their realm ID.
// Returns an empty list if there are no realms with that type.
func (d *ServiceDB) LoadAuthRealmsByType(realmType string) (realms []types.AuthRealm, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		realms, err = selectRealmsByTypeTxn(txn, realmType)
		return err
	})
	return
}

// StoreAuthRealm stores the given AuthRealm, clobbering based on the realm ID.
// This function updates the time added/updated values. The previous realm, if any, is
// returned.
func (d *ServiceDB) StoreAuthRealm(realm types.AuthRealm) (old types.AuthRealm, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		old, err = selectRealmTxn(txn, realm.ID())
		if err == sql.ErrNoRows {
			return insertRealmTxn(txn, time.Now(), realm)
		} else if err != nil {
			return err
		} else {
			return updateRealmTxn(txn, time.Now(), realm)
		}
	})
	return
}

// StoreAuthSession stores the given AuthSession, clobbering based on the tuple of
// user ID and realm ID. This function updates the time added/updated values.
// The previous session, if any, is returned.
func (d *ServiceDB) StoreAuthSession(session types.AuthSession) (old types.AuthSession, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		old, err = selectAuthSessionByUserTxn(txn, session.RealmID(), session.UserID())
		if err == sql.ErrNoRows {
			return insertAuthSessionTxn(txn, time.Now(), session)
		} else if err != nil {
			return err
		} else {
			return updateAuthSessionTxn(txn, time.Now(), session)
		}
	})
	return
}

// LoadAuthSessionByUser loads an AuthSession from the database based on the given
// realm and user ID.
// Returns sql.ErrNoRows if the session isn't in the database.
func (d *ServiceDB) LoadAuthSessionByUser(realmID, userID string) (session types.AuthSession, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		session, err = selectAuthSessionByUserTxn(txn, realmID, userID)
		return err
	})
	return
}

// LoadAuthSessionByID loads an AuthSession from the database based on the given
// realm and session ID.
// Returns sql.ErrNoRows if the session isn't in the database.
func (d *ServiceDB) LoadAuthSessionByID(realmID, sessionID string) (session types.AuthSession, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		session, err = selectAuthSessionByIDTxn(txn, realmID, sessionID)
		return err
	})
	return
}

// LoadBotOptions loads bot options from the database.
// Returns sql.ErrNoRows if the bot options isn't in the database.
func (d *ServiceDB) LoadBotOptions(userID, roomID string) (opts types.BotOptions, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		opts, err = selectBotOptionsTxn(txn, userID, roomID)
		return err
	})
	return
}

// StoreBotOptions stores a BotOptions into the database either by inserting a new
// bot options or updating an existing bot options. Returns the old bot options if there
// was one.
func (d *ServiceDB) StoreBotOptions(opts types.BotOptions) (oldOpts types.BotOptions, err error) {
	err = runTransaction(d.db, func(txn *sql.Tx) error {
		oldOpts, err = selectBotOptionsTxn(txn, opts.UserID, opts.RoomID)
		if err == sql.ErrNoRows {
			return insertBotOptionsTxn(txn, time.Now(), opts)
		} else if err != nil {
			return err
		} else {
			return updateBotOptionsTxn(txn, time.Now(), opts)
		}
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
