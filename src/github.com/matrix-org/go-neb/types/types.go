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

// AuthRealm represents a place where a user can authenticate themselves.
// This may static (like github.com) or a specific domain (like matrix.org/jira)
type AuthRealm interface {
	ID() string
	Type() string
	OnReceiveRedirect(w http.ResponseWriter, req *http.Request)
	AuthSession(userID, realmID string) AuthSession
	RequestAuthSession(userID string, config json.RawMessage) interface{}
}

var realmsByType = map[string]func(string) AuthRealm{}

// RegisterAuthRealm registers a factory for creating AuthRealm instances.
func RegisterAuthRealm(factory func(string) AuthRealm) {
	realmsByType[factory("").Type()] = factory
}

// CreateAuthRealm creates an AuthRealm of the given type and realm ID.
// Returns nil if the realm couldn't be created.
func CreateAuthRealm(realmID, realmType string) AuthRealm {
	f := realmsByType[realmType]
	if f == nil {
		return nil
	}
	return f(realmID)
}

// AuthSession represents a single authentication session between a user and
// an auth realm.
type AuthSession interface {
	UserID() string
	RealmID() string
}
