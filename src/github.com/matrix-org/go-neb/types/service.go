package types

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/matrix-org/gomatrix"
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
	OnPoll(client *gomatrix.Client) time.Time
}

// A Service is the configuration for a bot service.
type Service interface {
	// Return the user ID of this service.
	ServiceUserID() string
	// Return an opaque ID used to identify this service.
	ServiceID() string
	// Return the type of service. This string MUST NOT change.
	ServiceType() string
	Commands(cli *gomatrix.Client) []Command
	Expansions(cli *gomatrix.Client) []Expansion
	OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *gomatrix.Client)
	// A lifecycle function which is invoked when the service is being registered. The old service, if one exists, is provided,
	// along with a Client instance for ServiceUserID(). If this function returns an error, the service will not be registered
	// or persisted to the database, and the user's request will fail. This can be useful if you depend on external factors
	// such as registering webhooks.
	Register(oldService Service, client *gomatrix.Client) error
	// A lifecycle function which is invoked after the service has been successfully registered and persisted to the database.
	// This function is invoked within the critical section for configuring services, guaranteeing that there will not be
	// concurrent modifications to this service whilst this function executes. This lifecycle hook should be used to clean
	// up resources which are no longer needed (e.g. removing old webhooks).
	PostRegister(oldService Service)
}

// DefaultService NO-OPs the implementation of optional Service interface methods. Feel free to override them.
type DefaultService struct {
	id            string
	serviceUserID string
	serviceType   string
}

// NewDefaultService creates a new service with implementations for ServiceID(), ServiceType() and ServiceUserID()
func NewDefaultService(serviceID, serviceUserID, serviceType string) DefaultService {
	return DefaultService{serviceID, serviceUserID, serviceType}
}

// ServiceID returns the service's ID. In order for this to return the ID, DefaultService MUST have been
// initialised by NewDefaultService, the zero-initialiser is NOT enough.
func (s *DefaultService) ServiceID() string {
	return s.id
}

// ServiceUserID returns the user ID that the service sends events as. In order for this to return the
// service user ID, DefaultService MUST have been initialised by NewDefaultService, the zero-initialiser
// is NOT enough.
func (s *DefaultService) ServiceUserID() string {
	return s.serviceUserID
}

// ServiceType returns the type of service. See each individual service package for the ServiceType constant
// to find out what this value actually is. In order for this to return the Type, DefaultService MUST have been
// initialised by NewDefaultService, the zero-initialiser is NOT enough.
func (s *DefaultService) ServiceType() string {
	return s.serviceType
}

// Commands returns no commands.
func (s *DefaultService) Commands(cli *gomatrix.Client) []Command {
	return []Command{}
}

// Expansions returns no expansions.
func (s *DefaultService) Expansions(cli *gomatrix.Client) []Expansion {
	return []Expansion{}
}

// Register does nothing and returns no error.
func (s *DefaultService) Register(oldService Service, client *gomatrix.Client) error { return nil }

// PostRegister does nothing.
func (s *DefaultService) PostRegister(oldService Service) {}

// OnReceiveWebhook does nothing but 200 OK the request.
func (s *DefaultService) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *gomatrix.Client) {
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
