package types

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"net/http"
	"strings"
	"time"
)

// BotOptions for a given bot user in a given room
type BotOptions struct {
	RoomID      string
	UserID      string
	SetByUserID string
	Options     map[string]interface{}
}

// Poller represents a thing which can poll. Services should implement this method signature to support polling.
type Poller interface {
	// OnPoll is called when the poller should poll. Return the timestamp when you want to be polled again.
	// Return 0 to never be polled again.
	OnPoll(client *matrix.Client) time.Time
}

// A Service is the configuration for a bot service.
type Service interface {
	// Return the user ID of this service.
	ServiceUserID() string
	// Return an opaque ID used to identify this service.
	ServiceID() string
	// Return the type of service. This string MUST NOT change.
	ServiceType() string
	Plugin(cli *matrix.Client, roomID string) plugin.Plugin
	OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client)
	// A lifecycle function which is invoked when the service is being registered. The old service, if one exists, is provided,
	// along with a Client instance for ServiceUserID(). If this function returns an error, the service will not be registered
	// or persisted to the database, and the user's request will fail. This can be useful if you depend on external factors
	// such as registering webhooks.
	Register(oldService Service, client *matrix.Client) error
	// A lifecycle function which is invoked after the service has been successfully registered and persisted to the database.
	// This function is invoked within the critical section for configuring services, guaranteeing that there will not be
	// concurrent modifications to this service whilst this function executes. This lifecycle hook should be used to clean
	// up resources which are no longer needed (e.g. removing old webhooks).
	PostRegister(oldService Service)
}

// DefaultService NO-OPs the implementation of optional Service interface methods. Feel free to override them.
type DefaultService struct{}

// Plugin returns no plugins.
func (s *DefaultService) Plugin(cli *matrix.Client, roomID string) plugin.Plugin {
	return plugin.Plugin{}
}

// Register does nothing and returns no error.
func (s *DefaultService) Register(oldService Service, client *matrix.Client) error { return nil }

// PostRegister does nothing.
func (s *DefaultService) PostRegister(oldService Service) {}

// OnReceiveWebhook does nothing but 200 OK the request.
func (s *DefaultService) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client) {
	w.WriteHeader(200) // Do nothing
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

var servicesByType = map[string]func(string, string, string) Service{}
var serviceTypesWhichPoll = map[string]bool{}

// RegisterService registers a factory for creating Service instances.
func RegisterService(factory func(string, string, string) Service) {
	s := factory("", "", "")
	servicesByType[s.ServiceType()] = factory

	if _, ok := s.(Poller); ok {
		serviceTypesWhichPoll[s.ServiceType()] = true
	}
}

// PollingServiceTypes returns a list of service types which meet the Poller interface
func PollingServiceTypes() (types []string) {
	for t := range serviceTypesWhichPoll {
		types = append(types, t)
	}
	return
}

// CreateService creates a Service of the given type and serviceID.
// Returns an error if the Service couldn't be created.
func CreateService(serviceID, serviceType, serviceUserID string, serviceJSON []byte) (Service, error) {
	f := servicesByType[serviceType]
	if f == nil {
		return nil, errors.New("Unknown service type: " + serviceType)
	}

	base64ServiceID := base64.RawURLEncoding.EncodeToString([]byte(serviceID))
	webhookEndpointURL := baseURL + "services/hooks/" + base64ServiceID
	service := f(serviceID, serviceUserID, webhookEndpointURL)
	if err := json.Unmarshal(serviceJSON, service); err != nil {
		return nil, err
	}
	return service, nil
}

// AuthRealm represents a place where a user can authenticate themselves.
// This may static (like github.com) or a specific domain (like matrix.org/jira)
type AuthRealm interface {
	ID() string
	Type() string
	Init() error
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
// Returns an error if the realm couldn't be created or the JSON cannot be unmarshalled.
func CreateAuthRealm(realmID, realmType string, realmJSON []byte) (AuthRealm, error) {
	f := realmsByType[realmType]
	if f == nil {
		return nil, errors.New("Unknown realm type: " + realmType)
	}
	base64RealmID := base64.RawURLEncoding.EncodeToString([]byte(realmID))
	redirectURL := baseURL + "realms/redirects/" + base64RealmID
	r := f(realmID, redirectURL)
	if err := json.Unmarshal(realmJSON, r); err != nil {
		return nil, err
	}
	if err := r.Init(); err != nil {
		return nil, err
	}
	return r, nil
}

// AuthSession represents a single authentication session between a user and
// an auth realm.
type AuthSession interface {
	ID() string
	UserID() string
	RealmID() string
	Authenticated() bool
	Info() interface{}
}
