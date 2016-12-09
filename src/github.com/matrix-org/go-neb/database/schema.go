package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/types"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS services (
	service_id TEXT NOT NULL,
	service_type TEXT NOT NULL,
	service_user_id TEXT NOT NULL,
	service_json TEXT NOT NULL,
	time_added_ms BIGINT NOT NULL,
	time_updated_ms BIGINT NOT NULL,
	UNIQUE(service_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS service_id_and_user_idx ON services(service_user_id, service_id);

CREATE TABLE IF NOT EXISTS matrix_clients (
	user_id TEXT NOT NULL,
	client_json TEXT NOT NULL,
	next_batch TEXT NOT NULL,
	time_added_ms BIGINT NOT NULL,
	time_updated_ms BIGINT NOT NULL,
	UNIQUE(user_id)
);

CREATE TABLE IF NOT EXISTS auth_realms (
	realm_id TEXT NOT NULL,
	realm_type TEXT NOT NULL,
	realm_json TEXT NOT NULL,
	time_added_ms BIGINT NOT NULL,
	time_updated_ms BIGINT NOT NULL,
	UNIQUE(realm_id)
);

CREATE TABLE IF NOT EXISTS auth_sessions (
	session_id TEXT NOT NULL,
	realm_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	session_json TEXT NOT NULL,
	time_added_ms BIGINT NOT NULL,
	time_updated_ms BIGINT NOT NULL,
	UNIQUE(realm_id, user_id),
	UNIQUE(realm_id, session_id)
);

CREATE TABLE IF NOT EXISTS bot_options (
	user_id TEXT NOT NULL,
	room_id TEXT NOT NULL,
	set_by_user_id TEXT NOT NULL,
	bot_options_json TEXT NOT NULL,
	time_added_ms BIGINT NOT NULL,
	time_updated_ms BIGINT NOT NULL,
	UNIQUE(user_id, room_id)
);
`

const selectMatrixClientConfigSQL = `
SELECT client_json FROM matrix_clients WHERE user_id = $1
`

func selectMatrixClientConfigTxn(txn *sql.Tx, userID string) (config api.ClientConfig, err error) {
	var configJSON []byte
	err = txn.QueryRow(selectMatrixClientConfigSQL, userID).Scan(&configJSON)
	if err != nil {
		return
	}
	err = json.Unmarshal(configJSON, &config)
	return
}

const selectMatrixClientConfigsSQL = `
SELECT client_json FROM matrix_clients
`

func selectMatrixClientConfigsTxn(txn *sql.Tx) (configs []api.ClientConfig, err error) {
	rows, err := txn.Query(selectMatrixClientConfigsSQL)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var config api.ClientConfig
		var configJSON []byte
		if err = rows.Scan(&configJSON); err != nil {
			return
		}
		if err = json.Unmarshal(configJSON, &config); err != nil {
			return
		}
		configs = append(configs, config)
	}
	return
}

const insertMatrixClientConfigSQL = `
INSERT INTO matrix_clients(
	user_id, client_json, next_batch, time_added_ms, time_updated_ms
) VALUES ($1, $2, '', $3, $4)
`

func insertMatrixClientConfigTxn(txn *sql.Tx, now time.Time, config api.ClientConfig) error {
	t := now.UnixNano() / 1000000
	configJSON, err := json.Marshal(&config)
	if err != nil {
		return err
	}
	_, err = txn.Exec(insertMatrixClientConfigSQL, config.UserID, configJSON, t, t)
	return err
}

const updateMatrixClientConfigSQL = `
UPDATE matrix_clients SET client_json = $1, time_updated_ms = $2
	WHERE user_id = $3
`

func updateMatrixClientConfigTxn(txn *sql.Tx, now time.Time, config api.ClientConfig) error {
	t := now.UnixNano() / 1000000
	configJSON, err := json.Marshal(&config)
	if err != nil {
		return err
	}
	_, err = txn.Exec(updateMatrixClientConfigSQL, configJSON, t, config.UserID)
	return err
}

const updateNextBatchSQL = `
UPDATE matrix_clients SET next_batch = $1 WHERE user_id = $2
`

func updateNextBatchTxn(txn *sql.Tx, userID, nextBatch string) error {
	_, err := txn.Exec(updateNextBatchSQL, nextBatch, userID)
	return err
}

const selectNextBatchSQL = `
SELECT next_batch FROM matrix_clients WHERE user_id = $1
`

func selectNextBatchTxn(txn *sql.Tx, userID string) (string, error) {
	var nextBatch string
	row := txn.QueryRow(selectNextBatchSQL, userID)
	if err := row.Scan(&nextBatch); err != nil {
		return "", err
	}
	return nextBatch, nil
}

const selectServiceSQL = `
SELECT service_type, service_user_id, service_json FROM services
	WHERE service_id = $1
`

func selectServiceTxn(txn *sql.Tx, serviceID string) (types.Service, error) {
	var serviceType string
	var serviceUserID string
	var serviceJSON []byte
	row := txn.QueryRow(selectServiceSQL, serviceID)
	if err := row.Scan(&serviceType, &serviceUserID, &serviceJSON); err != nil {
		return nil, err
	}
	return types.CreateService(serviceID, serviceType, serviceUserID, serviceJSON)
}

const updateServiceSQL = `
UPDATE services SET service_type=$1, service_user_id=$2, service_json=$3, time_updated_ms=$4
	WHERE service_id=$5
`

func updateServiceTxn(txn *sql.Tx, now time.Time, service types.Service) error {
	serviceJSON, err := json.Marshal(service)
	if err != nil {
		return err
	}
	t := now.UnixNano() / 1000000
	_, err = txn.Exec(
		updateServiceSQL, service.ServiceType(), service.ServiceUserID(), serviceJSON, t,
		service.ServiceID(),
	)
	return err
}

const insertServiceSQL = `
INSERT INTO services(
	service_id, service_type, service_user_id, service_json, time_added_ms, time_updated_ms
) VALUES ($1, $2, $3, $4, $5, $6)
`

func insertServiceTxn(txn *sql.Tx, now time.Time, service types.Service) error {
	serviceJSON, err := json.Marshal(service)
	if err != nil {
		return err
	}
	t := now.UnixNano() / 1000000
	_, err = txn.Exec(
		insertServiceSQL,
		service.ServiceID(), service.ServiceType(), service.ServiceUserID(), serviceJSON, t, t,
	)
	return err
}

const selectServicesForUserSQL = `
SELECT service_id, service_type, service_json FROM services WHERE service_user_id=$1 ORDER BY service_id
`

func selectServicesForUserTxn(txn *sql.Tx, userID string) (srvs []types.Service, err error) {
	rows, err := txn.Query(selectServicesForUserSQL, userID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var s types.Service
		var serviceID string
		var serviceType string
		var serviceJSON []byte
		if err = rows.Scan(&serviceID, &serviceType, &serviceJSON); err != nil {
			return
		}
		s, err = types.CreateService(serviceID, serviceType, userID, serviceJSON)
		if err != nil {
			return
		}
		srvs = append(srvs, s)
	}
	return
}

const selectServicesByTypeSQL = `
SELECT service_id, service_user_id, service_json FROM services WHERE service_type=$1 ORDER BY service_id
`

func selectServicesByTypeTxn(txn *sql.Tx, serviceType string) (srvs []types.Service, err error) {
	rows, err := txn.Query(selectServicesByTypeSQL, serviceType)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var s types.Service
		var serviceID string
		var serviceUserID string
		var serviceJSON []byte
		if err = rows.Scan(&serviceID, &serviceUserID, &serviceJSON); err != nil {
			return
		}
		s, err = types.CreateService(serviceID, serviceType, serviceUserID, serviceJSON)
		if err != nil {
			return
		}
		srvs = append(srvs, s)
	}
	return
}

const deleteServiceSQL = `
DELETE FROM services WHERE service_id = $1
`

func deleteServiceTxn(txn *sql.Tx, serviceID string) error {
	_, err := txn.Exec(deleteServiceSQL, serviceID)
	return err
}

const insertRealmSQL = `
INSERT INTO auth_realms(
	realm_id, realm_type, realm_json, time_added_ms, time_updated_ms
) VALUES ($1, $2, $3, $4, $5)
`

func insertRealmTxn(txn *sql.Tx, now time.Time, realm types.AuthRealm) error {
	realmJSON, err := json.Marshal(realm)
	if err != nil {
		return err
	}
	t := now.UnixNano() / 1000000
	_, err = txn.Exec(
		insertRealmSQL,
		realm.ID(), realm.Type(), realmJSON, t, t,
	)
	return err
}

const selectRealmSQL = `
SELECT realm_type, realm_json FROM auth_realms WHERE realm_id = $1
`

func selectRealmTxn(txn *sql.Tx, realmID string) (types.AuthRealm, error) {
	var realmType string
	var realmJSON []byte
	row := txn.QueryRow(selectRealmSQL, realmID)
	if err := row.Scan(&realmType, &realmJSON); err != nil {
		return nil, err
	}
	return types.CreateAuthRealm(realmID, realmType, realmJSON)
}

const selectRealmsByTypeSQL = `
SELECT realm_id, realm_json FROM auth_realms WHERE realm_type = $1 ORDER BY realm_id
`

func selectRealmsByTypeTxn(txn *sql.Tx, realmType string) (realms []types.AuthRealm, err error) {
	rows, err := txn.Query(selectRealmsByTypeSQL, realmType)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var realm types.AuthRealm
		var realmID string
		var realmJSON []byte
		if err = rows.Scan(&realmID, &realmJSON); err != nil {
			return
		}
		realm, err = types.CreateAuthRealm(realmID, realmType, realmJSON)
		if err != nil {
			return
		}
		realms = append(realms, realm)
	}
	return
}

const updateRealmSQL = `
UPDATE auth_realms SET realm_type=$1, realm_json=$2, time_updated_ms=$3
	WHERE realm_id=$4
`

func updateRealmTxn(txn *sql.Tx, now time.Time, realm types.AuthRealm) error {
	realmJSON, err := json.Marshal(realm)
	if err != nil {
		return err
	}
	t := now.UnixNano() / 1000000
	_, err = txn.Exec(
		updateRealmSQL, realm.Type(), realmJSON, t,
		realm.ID(),
	)
	return err
}

const insertAuthSessionSQL = `
INSERT INTO auth_sessions(
	session_id, realm_id, user_id, session_json, time_added_ms, time_updated_ms
) VALUES ($1, $2, $3, $4, $5, $6)
`

func insertAuthSessionTxn(txn *sql.Tx, now time.Time, session types.AuthSession) error {
	sessionJSON, err := json.Marshal(session)
	if err != nil {
		return err
	}
	t := now.UnixNano() / 1000000
	_, err = txn.Exec(
		insertAuthSessionSQL,
		session.ID(), session.RealmID(), session.UserID(), sessionJSON, t, t,
	)
	return err
}

const deleteAuthSessionSQL = `
DELETE FROM auth_sessions WHERE realm_id=$1 AND user_id=$2
`

func deleteAuthSessionTxn(txn *sql.Tx, realmID, userID string) error {
	_, err := txn.Exec(deleteAuthSessionSQL, realmID, userID)
	return err
}

const selectAuthSessionByUserSQL = `
SELECT session_id, realm_type, realm_json, session_json FROM auth_sessions
	JOIN auth_realms ON auth_sessions.realm_id = auth_realms.realm_id
	WHERE auth_sessions.realm_id = $1 AND auth_sessions.user_id = $2
`

func selectAuthSessionByUserTxn(txn *sql.Tx, realmID, userID string) (types.AuthSession, error) {
	var id string
	var realmType string
	var realmJSON []byte
	var sessionJSON []byte
	row := txn.QueryRow(selectAuthSessionByUserSQL, realmID, userID)
	if err := row.Scan(&id, &realmType, &realmJSON, &sessionJSON); err != nil {
		return nil, err
	}
	realm, err := types.CreateAuthRealm(realmID, realmType, realmJSON)
	if err != nil {
		return nil, err
	}
	session := realm.AuthSession(id, userID, realmID)
	if session == nil {
		return nil, fmt.Errorf("Cannot create session for given realm")
	}
	if err := json.Unmarshal(sessionJSON, session); err != nil {
		return nil, err
	}
	return session, nil
}

const selectAuthSessionByIDSQL = `
SELECT user_id, realm_type, realm_json, session_json FROM auth_sessions
	JOIN auth_realms ON auth_sessions.realm_id = auth_realms.realm_id
	WHERE auth_sessions.realm_id = $1 AND auth_sessions.session_id = $2
`

func selectAuthSessionByIDTxn(txn *sql.Tx, realmID, id string) (types.AuthSession, error) {
	var userID string
	var realmType string
	var realmJSON []byte
	var sessionJSON []byte
	row := txn.QueryRow(selectAuthSessionByIDSQL, realmID, id)
	if err := row.Scan(&userID, &realmType, &realmJSON, &sessionJSON); err != nil {
		return nil, err
	}
	realm, err := types.CreateAuthRealm(realmID, realmType, realmJSON)
	if err != nil {
		return nil, err
	}
	session := realm.AuthSession(id, userID, realmID)
	if session == nil {
		return nil, fmt.Errorf("Cannot create session for given realm")
	}
	if err := json.Unmarshal(sessionJSON, session); err != nil {
		return nil, err
	}
	return session, nil
}

const updateAuthSessionSQL = `
UPDATE auth_sessions SET session_id=$1, session_json=$2, time_updated_ms=$3
	WHERE realm_id=$4 AND user_id=$5
`

func updateAuthSessionTxn(txn *sql.Tx, now time.Time, session types.AuthSession) error {
	sessionJSON, err := json.Marshal(session)
	if err != nil {
		return err
	}
	t := now.UnixNano() / 1000000
	_, err = txn.Exec(
		updateAuthSessionSQL, session.ID(), sessionJSON, t,
		session.RealmID(), session.UserID(),
	)
	return err
}

const selectBotOptionsSQL = `
SELECT bot_options_json, set_by_user_id FROM bot_options WHERE user_id = $1 AND room_id = $2
`

func selectBotOptionsTxn(txn *sql.Tx, userID, roomID string) (opts types.BotOptions, err error) {
	var optionsJSON []byte
	err = txn.QueryRow(selectBotOptionsSQL, userID, roomID).Scan(&optionsJSON, &opts.SetByUserID)
	if err != nil {
		return
	}
	err = json.Unmarshal(optionsJSON, &opts.Options)
	return
}

const insertBotOptionsSQL = `
INSERT INTO bot_options(
	user_id, room_id, bot_options_json, set_by_user_id, time_added_ms, time_updated_ms
) VALUES ($1, $2, $3, $4, $5, $6)
`

func insertBotOptionsTxn(txn *sql.Tx, now time.Time, opts types.BotOptions) error {
	t := now.UnixNano() / 1000000
	optsJSON, err := json.Marshal(&opts.Options)
	if err != nil {
		return err
	}
	_, err = txn.Exec(insertBotOptionsSQL, opts.UserID, opts.RoomID, optsJSON, opts.SetByUserID, t, t)
	return err
}

const updateBotOptionsSQL = `
UPDATE bot_options SET bot_options_json = $1, set_by_user_id = $2, time_updated_ms = $3
	WHERE user_id = $4 AND room_id = $5
`

func updateBotOptionsTxn(txn *sql.Tx, now time.Time, opts types.BotOptions) error {
	t := now.UnixNano() / 1000000
	optsJSON, err := json.Marshal(&opts.Options)
	if err != nil {
		return err
	}
	_, err = txn.Exec(updateBotOptionsSQL, optsJSON, opts.SetByUserID, t, opts.UserID, opts.RoomID)
	return err
}
