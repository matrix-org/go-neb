package realms

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	"io/ioutil"
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
	AccessToken string
	Scopes      string
	id          string
	userID      string
	realmID     string
}

func (s *githubSession) UserID() string {
	return s.userID
}

func (s *githubSession) RealmID() string {
	return s.realmID
}

func (s *githubSession) ID() string {
	return s.id
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
		id:      state, // key off the state for redirects
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
	// parse out params from the request
	code := req.URL.Query().Get("code")
	state := req.URL.Query().Get("state")
	logger := log.WithFields(log.Fields{
		"state": state,
	})
	logger.WithField("code", code).Print("GithubRealm: OnReceiveRedirect")
	if code == "" || state == "" {
		failWith(logger, w, 400, "code and state are required", nil)
		return
	}
	// load the session (we keyed off the state param)
	session, err := database.GetServiceDB().LoadAuthSessionByID(r.ID(), state)
	if err != nil {
		// most likely cause
		failWith(logger, w, 400, "Provided ?state= param is not recognised.", err)
		return
	}
	ghSession := session.(*githubSession)
	logger.WithField("user_id", ghSession.UserID()).Print("Mapped redirect to user")

	// exchange code for access_token
	res, err := http.PostForm("https://github.com/login/oauth/access_token",
		url.Values{"client_id": {r.ClientID}, "client_secret": {r.ClientSecret}, "code": {code}})
	if err != nil {
		failWith(logger, w, 502, "Failed to exchange code for token", err)
		return
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		failWith(logger, w, 502, "Failed to read token response", err)
		return
	}
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		failWith(logger, w, 502, "Failed to parse token response", err)
		return
	}

	// update database and return
	ghSession.AccessToken = vals.Get("access_token")
	ghSession.Scopes = vals.Get("scope")
	logger.WithField("scope", ghSession.Scopes).Print("Scopes granted.")
	_, err = database.GetServiceDB().StoreAuthSession(ghSession)
	if err != nil {
		failWith(logger, w, 500, "Failed to persist session", err)
		return
	}
	w.WriteHeader(200)
	w.Write([]byte("OK!"))
}

func (r *githubRealm) AuthSession(id, userID, realmID string) types.AuthSession {
	return &githubSession{
		id:      id,
		userID:  userID,
		realmID: realmID,
	}
}

func failWith(logger *log.Entry, w http.ResponseWriter, code int, msg string, err error) {
	logger.WithError(err).Print(msg)
	w.WriteHeader(code)
	w.Write([]byte(msg))
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
