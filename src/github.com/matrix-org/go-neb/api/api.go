package api

import (
	"encoding/json"
	"errors"
	"net/url"
)

// ConfigureAuthRealmRequest is a request to /configureAuthRealm
type ConfigureAuthRealmRequest struct {
	ID     string
	Type   string
	Config json.RawMessage
}

// ConfigureServiceRequest is a request to /configureService
type ConfigureServiceRequest struct {
	ID     string
	Type   string
	UserID string
	Config json.RawMessage
}

// A ClientConfig is the configuration for a matrix client for a bot to use. It is
// a request to /configureClient
type ClientConfig struct {
	UserID        string // The matrix UserId to connect with.
	HomeserverURL string // A URL with the host and port of the matrix server. E.g. https://matrix.org:8448
	AccessToken   string // The matrix access token to authenticate the requests with.
	Sync          bool   // True to start a sync stream for this user
	AutoJoinRooms bool   // True to automatically join all rooms for this user
	DisplayName   string // The display name to set for the matrix client
}

// SessionRequest are usually multi-stage things so this type only exists for the config form
type SessionRequest struct {
	SessionID string
	RealmID   string
	UserID    string
	Config    json.RawMessage
}

// ConfigFile represents config.sample.yaml
type ConfigFile struct {
	Clients  []ClientConfig
	Realms   []ConfigureAuthRealmRequest
	Services []ConfigureServiceRequest
	Sessions []SessionRequest
}

// Check validates the /configureService request
func (c *ConfigureServiceRequest) Check() error {
	if c.ID == "" || c.Type == "" || c.UserID == "" || c.Config == nil {
		return errors.New(`Must supply an "ID", a "Type", a "UserID" and a "Config"`)
	}
	return nil
}

// Check validates the /configureAuthRealm request
func (c *ConfigureAuthRealmRequest) Check() error {
	if c.ID == "" || c.Type == "" || c.Config == nil {
		return errors.New(`Must supply a "ID", a "Type" and a "Config"`)
	}
	return nil
}

// Check validates the session config request
func (c *SessionRequest) Check() error {
	if c.SessionID == "" || c.UserID == "" || c.RealmID == "" || c.Config == nil {
		return errors.New(`Must supply a "SessionID", a "RealmID", a "UserID" and a "Config"`)
	}
	return nil
}

// Check that the client has the correct fields.
func (c *ClientConfig) Check() error {
	if c.UserID == "" || c.HomeserverURL == "" || c.AccessToken == "" {
		return errors.New(`Must supply a "UserID", a "HomeserverURL", and an "AccessToken"`)
	}
	if _, err := url.Parse(c.HomeserverURL); err != nil {
		return err
	}
	return nil
}
