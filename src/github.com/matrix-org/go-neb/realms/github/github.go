package realms

import (
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
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
	State   string
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

func (r *githubRealm) RequestAuthSession(userID string, req json.RawMessage) interface{} {
	u, _ := url.Parse("https://github.com/login/oauth/authorize")
	q := u.Query()
	q.Set("client_id", r.ClientID)
	q.Set("client_secret", r.ClientSecret)
	// TODO: state, scope
	u.RawQuery = q.Encode()
	session := &githubSession{
		State:   "TODO",
		userID:  userID,
		realmID: r.ID(),
	}
	_, err := database.GetServiceDB().StoreAuthSession(session)
	if err != nil {
		log.WithError(err).Print("Failed to store new auth session")
		return nil
	}

	return &struct {
		URL string
	}{u.String()}
}

func (r *githubRealm) AuthSession(userID, realmID string) types.AuthSession {
	return &githubSession{
		userID:  userID,
		realmID: realmID,
	}
}

func init() {
	types.RegisterAuthRealm(func(realmID string) types.AuthRealm {
		return &githubRealm{id: realmID}
	})
}
