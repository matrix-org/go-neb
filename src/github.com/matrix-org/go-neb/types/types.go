package types

import (
	"encoding/json"
	"errors"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"net/http"
	"net/url"
)

// A ClientConfig is the configuration for a matrix client for a bot to use.
type ClientConfig struct {
	UserID        string // The matrix UserId to connect with.
	HomeserverURL string // A URL with the host and port of the matrix server. E.g. https://matrix.org:8448
	AccessToken   string // The matrix access token to authenticate the requests with.
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

// A Service is the configuration for a bot service.
type Service interface {
	ServiceUserID() string
	ServiceID() string
	ServiceType() string
	RoomIDs() []string
	Plugin(roomID string) plugin.Plugin
	OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client)
}

var servicesByType = map[string]func(string) Service{}

// RegisterService registers a factory for creating Service instances.
func RegisterService(factory func(string) Service) {
	servicesByType[factory("").ServiceType()] = factory
}

// CreateService creates a Service of the given type and serviceID.
// Returns nil if the Service couldn't be created.
func CreateService(serviceID, serviceType string) Service {
	f := servicesByType[serviceType]
	if f == nil {
		return nil
	}
	return f(serviceID)
}

// AuthModule represents a thing which can handle auth requests of a given type.
type AuthModule interface {
	Type() string
	Process(tpa ThirdPartyAuth) error
}

var authModulesByType = map[string]AuthModule{}

// ThirdPartyAuth represents an individual authorisation entry between
// a third party and the Matrix user.
type ThirdPartyAuth struct {
	// The ID of the matrix user who has authed with the third party
	UserID string
	// The type of auth (e.g. "jira", "github"). This determines which
	// auth module is loaded to process the auth.
	Type string
	// The location of the third party resource e.g. "github.com".
	// This is mainly relevant for decentralised services like JIRA which
	// may have many different locations (e.g. "matrix.org/jira") for the
	// same ServiceType ("jira").
	Resource string
	// An opaque JSON blob of stored auth data.
	AuthJSON json.RawMessage
}

// RegisterAuthModule so it can be used by other parts of NEB.
func RegisterAuthModule(am AuthModule) {
	authModulesByType[am.Type()] = am
}

// GetAuthModule for the given auth type. Returns nil if no match.
func GetAuthModule(authType string) AuthModule {
	return authModulesByType[authType]
}
