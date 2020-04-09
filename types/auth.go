package types

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
)

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
