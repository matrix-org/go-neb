package api

import (
	"encoding/json"
	"errors"
	"net/url"
)

type ConfigureAuthRealmRequest struct {
	ID     string
	Type   string
	Config json.RawMessage
}

type ConfigureServiceRequest struct {
	ID     string
	Type   string
	UserID string
	Config json.RawMessage
}

// A ClientConfig is the configuration for a matrix client for a bot to use.
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
