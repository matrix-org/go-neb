package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/matrix-org/go-neb/types"
	"time"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS services (
	service_id TEXT NOT NULL,
	service_type TEXT NOT NULL,
	service_json TEXT NOT NULL,
	time_added_ms BIGINT NOT NULL,
	time_updated_ms BIGINT NOT NULL,
	UNIQUE(service_id)
);

CREATE TABLE IF NOT EXISTS rooms_to_services (
	service_user_id TEXT NOT NULL,
	room_id TEXT NOT NULL,
	service_id TEXT NOT NULL,
	time_added_ms BIGINT NOT NULL,
	UNIQUE(service_user_id, room_id, service_id)
);

CREATE TABLE IF NOT EXISTS matrix_clients (
	user_id TEXT NOT NULL,
	client_json TEXT NOT NULL,
	next_batch TEXT NOT NULL,
	time_added_ms BIGINT NOT NULL,
	time_updated_ms BIGINT NOT NULL,
	UNIQUE(user_id)
);
`

const selectServiceUserIDsSQL = `
SELECT service_user_id, room_id FROM rooms_to_services
	GROUP BY service_user_id, room_id
`

// selectServiceUserIDsTxn returns a map from userIDs to lists of roomIDs.
func selectServiceUserIDsTxn(txn *sql.Tx) (map[string][]string, error) {
	rows, err := txn.Query(selectServiceUserIDsSQL)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]string)
	for rows.Next() {
		var uID, rID string
		if err = rows.Scan(&uID, &rID); err != nil {
			return nil, err
		}
		result[uID] = append(result[uID], rID)
	}
	return result, nil
}

const selectMatrixClientConfigSQL = `
SELECT client_json FROM matrix_clients WHERE user_id = $1
`

func selectMatrixClientConfigTxn(txn *sql.Tx, userID string) (config types.ClientConfig, err error) {
	var configJSON []byte
	err = txn.QueryRow(selectMatrixClientConfigSQL, userID).Scan(&configJSON)
	if err != nil {
		return
	}
	err = json.Unmarshal(configJSON, &config)
	return
}

const insertMatrixClientConfigSQL = `
INSERT INTO matrix_clients(
	user_id, client_json, next_batch, time_added_ms, time_updated_ms
) VALUES ($1, $2, '', $3, $4)
`

func insertMatrixClientConfigTxn(txn *sql.Tx, now time.Time, config types.ClientConfig) error {
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

func updateMatrixClientConfigTxn(txn *sql.Tx, now time.Time, config types.ClientConfig) error {
	t := now.UnixNano() / 1000000
	configJSON, err := json.Marshal(&config)
	if err != nil {
		return err
	}
	_, err = txn.Exec(updateMatrixClientConfigSQL, configJSON, t, config.UserID)
	return err
}

const selectServiceSQL = `
SELECT service_type, service_json FROM services
	WHERE service_id = $1
`

func selectServiceTxn(txn *sql.Tx, serviceID string) (types.Service, error) {
	var serviceType string
	var serviceJSON []byte
	row := txn.QueryRow(selectServiceSQL, serviceID)
	if err := row.Scan(&serviceType, &serviceJSON); err != nil {
		return nil, err
	}
	service := types.CreateService(serviceID, serviceType)
	if service == nil {
		return nil, fmt.Errorf("Cannot create services of type %s", serviceType)
	}
	if err := json.Unmarshal(serviceJSON, service); err != nil {
		return nil, err
	}
	return service, nil
}

const updateServiceSQL = `
UPDATE services SET service_type=$1, service_json=$2, time_updated_ms=$3
	WHERE service_id=$4
`

func updateServiceTxn(txn *sql.Tx, now time.Time, service types.Service) error {
	serviceJSON, err := json.Marshal(service)
	if err != nil {
		return err
	}
	t := now.UnixNano() / 1000000
	_, err = txn.Exec(
		updateServiceSQL, service.ServiceType(), serviceJSON, t,
		service.ServiceID(),
	)
	return err
}

const insertServiceSQL = `
INSERT INTO services(
	service_id, service_type, service_json, time_added_ms, time_updated_ms
) VALUES ($1, $2, $3, $4, $5)
`

func insertServiceTxn(txn *sql.Tx, now time.Time, service types.Service) error {
	serviceJSON, err := json.Marshal(service)
	if err != nil {
		return err
	}
	t := now.UnixNano() / 1000000
	_, err = txn.Exec(
		insertServiceSQL,
		service.ServiceID(), service.ServiceType(), serviceJSON, t, t,
	)
	return err
}

const insertRoomServiceSQL = `
INSERT INTO rooms_to_services(service_user_id, room_id, service_id, time_added_ms)
	VALUES ($1, $2, $3, $4)
`

func insertRoomServiceTxn(txn *sql.Tx, now time.Time, serviceUserID, roomID, serviceID string) error {
	t := now.UnixNano() / 1000000
	_, err := txn.Exec(insertRoomServiceSQL, serviceUserID, roomID, serviceID, t)
	return err
}

const deleteRoomServiceSQL = `
DELETE FROM rooms_to_services WHERE service_user_id=$1 AND room_id = $2 AND service_id=$3
`

func deleteRoomServiceTxn(txn *sql.Tx, serviceUserID, roomID, serviceID string) error {
	_, err := txn.Exec(deleteRoomServiceSQL, serviceUserID, roomID, serviceID)
	return err
}

const selectRoomServicesSQL = `
SELECT service_id FROM rooms_to_services WHERE service_user_id=$1 AND room_id=$2
`

func selectRoomServicesTxn(txn *sql.Tx, serviceUserID, roomID string) (serviceIDs []string, err error) {
	rows, err := txn.Query(selectRoomServicesSQL, serviceUserID, roomID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var serviceID string
		if err = rows.Scan(&serviceID); err != nil {
			return
		}
		serviceIDs = append(serviceIDs, serviceID)
	}
	return
}
