package database

import (
	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/types"
)

// Storer is the interface which needs to be conformed to in order to persist Go-NEB data
type Storer interface {
	StoreMatrixClientConfig(config api.ClientConfig) (oldConfig api.ClientConfig, err error)
	LoadMatrixClientConfigs() (configs []api.ClientConfig, err error)
	LoadMatrixClientConfig(userID string) (config api.ClientConfig, err error)

	UpdateNextBatch(userID, nextBatch string) (err error)
	LoadNextBatch(userID string) (nextBatch string, err error)

	LoadService(serviceID string) (service types.Service, err error)
	DeleteService(serviceID string) (err error)
	LoadServicesForUser(serviceUserID string) (services []types.Service, err error)
	LoadServicesByType(serviceType string) (services []types.Service, err error)
	StoreService(service types.Service) (oldService types.Service, err error)

	LoadAuthRealm(realmID string) (realm types.AuthRealm, err error)
	LoadAuthRealmsByType(realmType string) (realms []types.AuthRealm, err error)
	StoreAuthRealm(realm types.AuthRealm) (old types.AuthRealm, err error)

	StoreAuthSession(session types.AuthSession) (old types.AuthSession, err error)
	LoadAuthSessionByUser(realmID, userID string) (session types.AuthSession, err error)
	LoadAuthSessionByID(realmID, sessionID string) (session types.AuthSession, err error)
	RemoveAuthSession(realmID, userID string) error

	LoadBotOptions(userID, roomID string) (opts types.BotOptions, err error)
	StoreBotOptions(opts types.BotOptions) (oldOpts types.BotOptions, err error)

	InsertFromConfig(cfg *api.ConfigFile) error
}

// NopStorage nops every store API call. This is intended to be embedded into derived structs
// in tests
type NopStorage struct{}

// StoreMatrixClientConfig NOP
func (s *NopStorage) StoreMatrixClientConfig(config api.ClientConfig) (oldConfig api.ClientConfig, err error) {
	return api.ClientConfig{}, nil
}

// LoadMatrixClientConfigs NOP
func (s *NopStorage) LoadMatrixClientConfigs() (configs []api.ClientConfig, err error) {
	return
}

// LoadMatrixClientConfig NOP
func (s *NopStorage) LoadMatrixClientConfig(userID string) (config api.ClientConfig, err error) {
	return
}

// UpdateNextBatch NOP
func (s *NopStorage) UpdateNextBatch(userID, nextBatch string) (err error) {
	return
}

// LoadNextBatch NOP
func (s *NopStorage) LoadNextBatch(userID string) (nextBatch string, err error) {
	return
}

// LoadService NOP
func (s *NopStorage) LoadService(serviceID string) (service types.Service, err error) {
	return
}

// DeleteService NOP
func (s *NopStorage) DeleteService(serviceID string) (err error) {
	return
}

// LoadServicesForUser NOP
func (s *NopStorage) LoadServicesForUser(serviceUserID string) (services []types.Service, err error) {
	return
}

// LoadServicesByType NOP
func (s *NopStorage) LoadServicesByType(serviceType string) (services []types.Service, err error) {
	return
}

// StoreService NOP
func (s *NopStorage) StoreService(service types.Service) (oldService types.Service, err error) {
	return
}

// LoadAuthRealm NOP
func (s *NopStorage) LoadAuthRealm(realmID string) (realm types.AuthRealm, err error) {
	return
}

// LoadAuthRealmsByType NOP
func (s *NopStorage) LoadAuthRealmsByType(realmType string) (realms []types.AuthRealm, err error) {
	return
}

// StoreAuthRealm NOP
func (s *NopStorage) StoreAuthRealm(realm types.AuthRealm) (old types.AuthRealm, err error) {
	return
}

// StoreAuthSession NOP
func (s *NopStorage) StoreAuthSession(session types.AuthSession) (old types.AuthSession, err error) {
	return
}

// LoadAuthSessionByUser NOP
func (s *NopStorage) LoadAuthSessionByUser(realmID, userID string) (session types.AuthSession, err error) {
	return
}

// LoadAuthSessionByID NOP
func (s *NopStorage) LoadAuthSessionByID(realmID, sessionID string) (session types.AuthSession, err error) {
	return
}

// RemoveAuthSession NOP
func (s *NopStorage) RemoveAuthSession(realmID, userID string) error {
	return nil
}

// LoadBotOptions NOP
func (s *NopStorage) LoadBotOptions(userID, roomID string) (opts types.BotOptions, err error) {
	return
}

// StoreBotOptions NOP
func (s *NopStorage) StoreBotOptions(opts types.BotOptions) (oldOpts types.BotOptions, err error) {
	return
}

// InsertFromConfig NOP
func (s *NopStorage) InsertFromConfig(cfg *api.ConfigFile) error {
	return nil
}
