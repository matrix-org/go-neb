package types

import (
	"encoding/json"
	"errors"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"net/http"
	"net/url"
	"strings"
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
	Register() error
	PostRegister(oldService Service)
}

var baseURL = ""

// BaseURL sets the base URL of NEB to the url given. This URL must be accessible from the
// public internet.
func BaseURL(u string) error {
	if u == "" {
		return errors.New("BASE_URL not found")
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return errors.New("BASE_URL must start with http[s]://")
	}
	if !strings.HasSuffix(u, "/") {
		u = u + "/"
	}
	baseURL = u
	return nil
}

var servicesByType = map[string]func(string, string) Service{}

// RegisterService registers a factory for creating Service instances.
func RegisterService(factory func(string, string) Service) {
	servicesByType[factory("", "").ServiceType()] = factory
}

// CreateService creates a Service of the given type and serviceID.
// Returns nil if the Service couldn't be created.
func CreateService(serviceID, serviceType string) Service {
	f := servicesByType[serviceType]
	if f == nil {
		return nil
	}
	webhookEndpointURL := baseURL + "services/hooks/" + serviceID
	return f(serviceID, webhookEndpointURL)
}

// AuthRealm represents a place where a user can authenticate themselves.
// This may static (like github.com) or a specific domain (like matrix.org/jira)
type AuthRealm interface {
	ID() string
	Type() string
	Register() error
	OnReceiveRedirect(w http.ResponseWriter, req *http.Request)
	AuthSession(id, userID, realmID string) AuthSession
	RequestAuthSession(userID string, config json.RawMessage) interface{}
}

var realmsByType = map[string]func(string, string) AuthRealm{}

// RegisterAuthRealm registers a factory for creating AuthRealm instances.
func RegisterAuthRealm(factory func(string, string) AuthRealm) {
	realmsByType[factory("", "").Type()] = factory
}

// CreateAuthRealm creates an AuthRealm of the given type and realm ID.
// Returns nil if the realm couldn't be created.
func CreateAuthRealm(realmID, realmType string) AuthRealm {
	f := realmsByType[realmType]
	if f == nil {
		return nil
	}
	redirectURL := baseURL + "realms/redirects/" + realmID
	return f(realmID, redirectURL)
}

// AuthSession represents a single authentication session between a user and
// an auth realm.
type AuthSession interface {
	ID() string
	UserID() string
	RealmID() string
}
