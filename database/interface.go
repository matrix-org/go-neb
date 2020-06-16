package database

import (
	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/types"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/id"
)

// Storer is the interface which needs to be conformed to in order to persist Go-NEB data
type Storer interface {
	crypto.Store

	StoreMatrixClientConfig(config api.ClientConfig) (oldConfig api.ClientConfig, err error)
	LoadMatrixClientConfigs() (configs []api.ClientConfig, err error)
	LoadMatrixClientConfig(userID id.UserID) (config api.ClientConfig, err error)

	UpdateNextBatch(userID id.UserID, nextBatch string) (err error)
	LoadNextBatch(userID id.UserID) (nextBatch string, err error)

	LoadService(serviceID string) (service types.Service, err error)
	DeleteService(serviceID string) (err error)
	LoadServicesForUser(serviceUserID id.UserID) (services []types.Service, err error)
	LoadServicesByType(serviceType string) (services []types.Service, err error)
	StoreService(service types.Service) (oldService types.Service, err error)

	LoadAuthRealm(realmID string) (realm types.AuthRealm, err error)
	LoadAuthRealmsByType(realmType string) (realms []types.AuthRealm, err error)
	StoreAuthRealm(realm types.AuthRealm) (old types.AuthRealm, err error)

	StoreAuthSession(session types.AuthSession) (old types.AuthSession, err error)
	LoadAuthSessionByUser(realmID string, userID id.UserID) (session types.AuthSession, err error)
	LoadAuthSessionByID(realmID, sessionID string) (session types.AuthSession, err error)
	RemoveAuthSession(realmID string, userID id.UserID) error

	LoadBotOptions(userID id.UserID, roomID id.RoomID) (opts types.BotOptions, err error)
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
func (s *NopStorage) LoadMatrixClientConfig(userID id.UserID) (config api.ClientConfig, err error) {
	return
}

// UpdateNextBatch NOP
func (s *NopStorage) UpdateNextBatch(userID id.UserID, nextBatch string) (err error) {
	return
}

// LoadNextBatch NOP
func (s *NopStorage) LoadNextBatch(userID id.UserID) (nextBatch string, err error) {
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
func (s *NopStorage) LoadServicesForUser(serviceUserID id.UserID) (services []types.Service, err error) {
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
func (s *NopStorage) LoadAuthSessionByUser(realmID string, userID id.UserID) (session types.AuthSession, err error) {
	return
}

// LoadAuthSessionByID NOP
func (s *NopStorage) LoadAuthSessionByID(realmID, sessionID string) (session types.AuthSession, err error) {
	return
}

// RemoveAuthSession NOP
func (s *NopStorage) RemoveAuthSession(realmID string, userID id.UserID) error {
	return nil
}

// LoadBotOptions NOP
func (s *NopStorage) LoadBotOptions(userID id.UserID, roomID id.RoomID) (opts types.BotOptions, err error) {
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

// PutAccount NOP
func (s *NopStorage) PutAccount(*crypto.OlmAccount) error {
	return nil
}

// GetAccount NOP
func (s *NopStorage) GetAccount() (*crypto.OlmAccount, error) {
	return nil, nil
}

// HasSession NOP
func (s *NopStorage) HasSession(id.SenderKey) bool {
	return false
}

// GetSessions NOP
func (s *NopStorage) GetSessions(id.SenderKey) (crypto.OlmSessionList, error) {
	return nil, nil
}

// GetLatestSession NOP
func (s *NopStorage) GetLatestSession(id.SenderKey) (*crypto.OlmSession, error) {
	return nil, nil
}

// AddSession NOP
func (s *NopStorage) AddSession(id.SenderKey, *crypto.OlmSession) error {
	return nil
}

// UpdateSession NOP
func (s *NopStorage) UpdateSession(id.SenderKey, *crypto.OlmSession) error {
	return nil
}

// PutGroupSession NOP
func (s *NopStorage) PutGroupSession(id.RoomID, id.SenderKey, id.SessionID, *crypto.InboundGroupSession) error {
	return nil
}

// GetGroupSession NOP
func (s *NopStorage) GetGroupSession(id.RoomID, id.SenderKey, id.SessionID) (*crypto.InboundGroupSession, error) {
	return nil, nil
}

// AddOutboundGroupSession NOP
func (s *NopStorage) AddOutboundGroupSession(*crypto.OutboundGroupSession) error {
	return nil
}

// UpdateOutboundGroupSession NOP
func (s *NopStorage) UpdateOutboundGroupSession(*crypto.OutboundGroupSession) error {
	return nil
}

// GetOutboundGroupSession NOP
func (s *NopStorage) GetOutboundGroupSession(id.RoomID) (*crypto.OutboundGroupSession, error) {
	return nil, nil
}

// RemoveOutboundGroupSession NOP
func (s *NopStorage) RemoveOutboundGroupSession(id.RoomID) error {
	return nil
}

// ValidateMessageIndex NOP
func (s *NopStorage) ValidateMessageIndex(senderKey id.SenderKey, sessionID id.SessionID, eventID id.EventID, index uint, timestamp int64) bool {
	return false
}

// GetDevices NOP
func (s *NopStorage) GetDevices(id.UserID) (map[id.DeviceID]*crypto.DeviceIdentity, error) {
	return nil, nil
}

// GetDevice NOP
func (s *NopStorage) GetDevice(id.UserID, id.DeviceID) (*crypto.DeviceIdentity, error) {
	return nil, nil
}

// PutDevices NOP
func (s *NopStorage) PutDevices(id.UserID, map[id.DeviceID]*crypto.DeviceIdentity) error {
	return nil
}

// FilterTrackedUsers NOP
func (s *NopStorage) FilterTrackedUsers([]id.UserID) []id.UserID {
	return nil
}

// Flush NOP
func (s *NopStorage) Flush() error {
	return nil
}
