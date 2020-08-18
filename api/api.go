// Package api contains the fundamental data types used by Go-NEB.
//
// Most HTTP API calls and/or config file sections are just ways of representing these
// data types.
//
// See also
//
// Package "api.handlers" for information on the HTTP API calls.
package api

import (
	"encoding/json"
	"errors"
	"net/url"

	"maunium.net/go/mautrix/id"
)

// ConfigureAuthRealmRequest is a request to /configureAuthRealm
type ConfigureAuthRealmRequest struct {
	// An arbitrary unique identifier for this auth realm. This can be anything.
	// Using an existing ID will REPLACE the entire existing AuthRealm with the new information.
	ID string
	// The type of auth realm. This determines which code is loaded to execute the
	// auth realm. It must be a known type. E.g. "github".
	Type string
	// AuthRealm specific config information. See the docs for the auth realm you're interested in.
	Config json.RawMessage
}

// RequestAuthSessionRequest is a request to /requestAuthSession
type RequestAuthSessionRequest struct {
	// The realm ID to request a new auth session on. The realm MUST already exist.
	RealmID string
	// The Matrix user ID requesting the auth session. If the auth is successful,
	// this user ID will be associated with the third-party credentials obtained.
	UserID id.UserID
	// AuthRealm specific config information. See the docs for the auth realm you're interested in.
	Config json.RawMessage
}

// ConfigureServiceRequest is a request to /configureService
type ConfigureServiceRequest struct {
	// An arbitrary unique identifier for this service. This can be anything.
	// Using an existing ID will REPLACE the entire Service with the new information.
	ID string
	// The type of service. This determines which code is loaded to execute the
	// service. It must be a known type, e.g. "github".
	Type string
	// The user ID of the configured client that this service will use to communicate with Matrix.
	// The user MUST already be configured.
	UserID id.UserID
	// Service-specific config information. See the docs for the service you're interested in.
	Config json.RawMessage
}

// A ClientConfig contains the configuration information for a matrix client so that
// Go-NEB can drive it. It forms the HTTP body to /configureClient requests.
type ClientConfig struct {
	// The matrix User ID to connect with. E.g. @alice:matrix.org
	UserID id.UserID
	// A URL with the host and port of the matrix server. E.g. https://matrix.org:8448
	HomeserverURL string
	// The matrix access token to authenticate the requests with.
	AccessToken string
	// The device ID for this access token.
	DeviceID id.DeviceID
	// True to start a sync stream for this user, making this a "syncing client". If false, no
	// /sync goroutine will be created and this client won't listen for new events from Matrix. For services
	// which only SEND events into Matrix, it may be desirable to set Sync to false to reduce the
	// number of goroutines Go-NEB has to maintain. For services which respond to !commands,
	// Sync MUST be set to true in order to receive those commands.
	Sync bool
	// True to automatically join every room this client is invited to.
	// This is desirable for services which have !commands as that means anyone can pull the bot
	// into the room. It is up to the service to decide which, if any, users to respond to however.
	AutoJoinRooms bool
	// The desired display name for this client.
	// This does not automatically set the display name for this client. See /configureClient.
	DisplayName string
	// A list of regexes that control which users are allowed to start a SAS verification with this client.
	// When a user starts a new SAS verification with us, their user ID has to match one of these regexes
	// for the verification process to start.
	AcceptVerificationFromUsers []string
}

// A IncomingDecimalSAS contains the decimal SAS as displayed on another device. The SAS consists of three numbers.
type IncomingDecimalSAS struct {
	// The matrix User ID of the user that Neb uses in the verification process. E.g. @neb:localhost
	UserID id.UserID
	// The three numbers that the SAS consists of.
	SAS [3]uint
	// The matrix User ID of the other user whose device is being verified.
	OtherUserID id.UserID
	// The matrix Device ID of the other device that is being verified.
	OtherDeviceID id.DeviceID
}

// Session contains the complete auth session information for a given user on a given realm.
// They are created for use with ConfigFile.
type Session struct {
	SessionID string
	RealmID   string
	UserID    id.UserID
	Config    json.RawMessage
}

// ConfigFile represents config.sample.yaml
type ConfigFile struct {
	Clients  []ClientConfig
	Realms   []ConfigureAuthRealmRequest
	Services []ConfigureServiceRequest
	Sessions []Session
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
func (c *Session) Check() error {
	if c.SessionID == "" || c.UserID == "" || c.RealmID == "" || c.Config == nil {
		return errors.New(`Must supply a "SessionID", a "RealmID", a "UserID" and a "Config"`)
	}
	return nil
}

// Check that the client has supplied the correct fields.
func (c *ClientConfig) Check() error {
	if c.UserID == "" || c.HomeserverURL == "" || c.AccessToken == "" {
		return errors.New(`Must supply a "UserID", a "HomeserverURL", and an "AccessToken"`)
	}
	if _, err := url.Parse(c.HomeserverURL); err != nil {
		return err
	}
	return nil
}

// Check that the received SAS data contains the correct fields.
func (c *IncomingDecimalSAS) Check() error {
	if c.UserID == "" || c.OtherUserID == "" || c.OtherDeviceID == "" {
		return errors.New(`Must supply a "UserID", an "OtherUserID", and an "OtherDeviceID"`)
	}
	return nil
}

// Check that the request is valid.
func (r *RequestAuthSessionRequest) Check() error {
	if r.UserID == "" || r.RealmID == "" || r.Config == nil {
		return errors.New(`Must supply a "UserID", a "RealmID" and a "Config"`)
	}
	return nil
}
