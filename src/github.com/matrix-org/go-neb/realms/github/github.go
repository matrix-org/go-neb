package realms

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	"net/http"
	"net/url"
)

type githubRealm struct {
	id              string
	ClientSecret    string
	ClientID        string
	RedirectBaseURI string
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
	state, err := randomString(10)
	if err != nil {
		log.WithError(err).Print("Failed to generate state param")
		return nil
	}
	u, _ := url.Parse("https://github.com/login/oauth/authorize")
	q := u.Query()
	q.Set("client_id", r.ClientID)
	q.Set("client_secret", r.ClientSecret)
	q.Set("state", state)
	// TODO: Path is from goneb.go - we should probably factor it out.
	q.Set("redirect_uri", r.RedirectBaseURI+"/realms/redirects/"+r.ID())
	u.RawQuery = q.Encode()
	session := &githubSession{
		State:   state,
		userID:  userID,
		realmID: r.ID(),
	}
	_, err = database.GetServiceDB().StoreAuthSession(session)
	if err != nil {
		log.WithError(err).Print("Failed to store new auth session")
		return nil
	}

	return &struct {
		URL string
	}{u.String()}
}

func (r *githubRealm) OnReceiveRedirect(w http.ResponseWriter, req *http.Request) {
}

func (r *githubRealm) AuthSession(userID, realmID string) types.AuthSession {
	return &githubSession{
		userID:  userID,
		realmID: realmID,
	}
}

// Generate a cryptographically secure pseudorandom string with the given number of bytes (length).
// Returns a hex string of the bytes.
func randomString(length int) (string, error) {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func init() {
	types.RegisterAuthRealm(func(realmID string) types.AuthRealm {
		return &githubRealm{id: realmID}
	})
}
