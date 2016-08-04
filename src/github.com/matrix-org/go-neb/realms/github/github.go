package realms

import (
	"encoding/json"
	"github.com/matrix-org/go-neb/types"
	"net/url"
)

type githubRealm struct {
	id              string
	ClientSecret    string
	ClientID        string
	WebhookEndpoint string
}

type githubSession struct {
	URL     string
	userID  string
	realmID string
}

func (s *githubSession) UserID() string {
	return s.userID
}

func (s *githubSession) RealmID() string {
	return s.realmID
}

func (r *githubRealm) ID() string {
	return r.id
}

func (r *githubRealm) Type() string {
	return "github"
}

func (r *githubRealm) AuthSession(userID string, config json.RawMessage) types.AuthSession {
	u, _ := url.Parse("https://github.com/login/oauth/authorize")
	q := u.Query()
	q.Set("client_id", r.ClientID)
	q.Set("client_secret", r.ClientSecret)
	// TODO: state, scope
	u.RawQuery = q.Encode()
	return &githubSession{
		URL:     u.String(),
		userID:  userID,
		realmID: r.ID(),
	}
}

func init() {
	types.RegisterAuthRealm(func(realmID string) types.AuthRealm {
		return &githubRealm{id: realmID}
	})
}
