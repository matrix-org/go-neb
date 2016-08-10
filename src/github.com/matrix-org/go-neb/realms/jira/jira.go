package realms

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	log "github.com/Sirupsen/logrus"
	"github.com/andygrunwald/go-jira"
	"github.com/dghubble/oauth1"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/realms/jira/urls"
	"github.com/matrix-org/go-neb/types"
	"golang.org/x/net/context"
	"net/http"
)

type jiraRealm struct {
	id             string
	redirectURL    string
	privateKey     *rsa.PrivateKey
	JIRAEndpoint   string
	Server         string // clobbered based on /serverInfo request
	Version        string // clobbered based on /serverInfo request
	ConsumerName   string
	ConsumerKey    string
	ConsumerSecret string
	PublicKeyPEM   string // clobbered based on PrivateKeyPEM
	PrivateKeyPEM  string
}

// JIRASession represents a single authentication session between a user and a JIRA endpoint.
// The endpoint is dictated by the realm ID.
type JIRASession struct {
	id            string // request token
	userID        string
	realmID       string
	RequestSecret string
	AccessToken   string
	AccessSecret  string
}

// UserID returns the ID of the user performing the authentication.
func (s *JIRASession) UserID() string {
	return s.userID
}

// RealmID returns the JIRA realm ID which created this session.
func (s *JIRASession) RealmID() string {
	return s.realmID
}

// ID returns the OAuth1 request_token which is used when looking up sessions in the redirect
// handler.
func (s *JIRASession) ID() string {
	return s.id
}

func (r *jiraRealm) ID() string {
	return r.id
}

func (r *jiraRealm) Type() string {
	return "jira"
}

func (r *jiraRealm) Register() error {
	if r.ConsumerName == "" || r.ConsumerKey == "" || r.ConsumerSecret == "" || r.PrivateKeyPEM == "" {
		return errors.New("ConsumerName, ConsumerKey, ConsumerSecret, PrivateKeyPEM must be specified.")
	}
	if r.JIRAEndpoint == "" {
		return errors.New("JIRAEndpoint must be specified")
	}

	if err := r.ensureInited(); err != nil {
		return err
	}

	// Check to see if JIRA endpoint is valid by pinging an endpoint
	cli, err := r.jiraClient(r.JIRAEndpoint, "", true)
	if err != nil {
		return err
	}
	info, err := jiraServerInfo(cli)
	if err != nil {
		return err
	}
	log.WithFields(log.Fields{
		"jira_url": r.JIRAEndpoint,
		"title":    info.ServerTitle,
		"version":  info.Version,
	}).Print("Found JIRA endpoint")
	r.Server = info.ServerTitle
	r.Version = info.Version

	return nil
}

func (r *jiraRealm) RequestAuthSession(userID string, req json.RawMessage) interface{} {
	logger := log.WithField("jira_url", r.JIRAEndpoint)
	if err := r.ensureInited(); err != nil {
		logger.WithError(err).Print("Failed to init realm")
		return nil
	}
	authConfig := r.oauth1Config(r.JIRAEndpoint)
	reqToken, reqSec, err := authConfig.RequestToken()
	if err != nil {
		logger.WithError(err).Print("Failed to request auth token")
		return nil
	}
	logger.WithField("req_token", reqToken).Print("Received request token")
	authURL, err := authConfig.AuthorizationURL(reqToken)
	if err != nil {
		logger.WithError(err).Print("Failed to create authorization URL")
		return nil
	}

	_, err = database.GetServiceDB().StoreAuthSession(&JIRASession{
		id:            reqToken,
		userID:        userID,
		realmID:       r.id,
		RequestSecret: reqSec,
	})
	if err != nil {
		log.WithError(err).Print("Failed to store new auth session")
		return nil
	}

	return &struct {
		URL string
	}{authURL.String()}
}

func (r *jiraRealm) OnReceiveRedirect(w http.ResponseWriter, req *http.Request) {
	logger := log.WithField("jira_url", r.JIRAEndpoint)
	if err := r.ensureInited(); err != nil {
		failWith(logger, w, 500, "Failed to initialise realm", err)
		return
	}

	requestToken, verifier, err := oauth1.ParseAuthorizationCallback(req)
	if err != nil {
		failWith(logger, w, 400, "Failed to parse authorization callback", err)
		return
	}
	logger = logger.WithField("req_token", requestToken)
	logger.Print("Received authorization callback")

	session, err := database.GetServiceDB().LoadAuthSessionByID(r.id, requestToken)
	if err != nil {
		failWith(logger, w, 400, "Unrecognised request token", err)
		return
	}
	jiraSession, ok := session.(*JIRASession)
	if !ok {
		failWith(logger, w, 500, "Unexpected session type found.", nil)
		return
	}
	logger = logger.WithField("user_id", jiraSession.UserID())
	logger.Print("Retrieved auth session for user")

	oauthConfig := r.oauth1Config(r.JIRAEndpoint)
	accessToken, accessSecret, err := oauthConfig.AccessToken(requestToken, jiraSession.RequestSecret, verifier)
	if err != nil {
		failWith(logger, w, 502, "Failed exchange for access token.", err)
		return
	}
	logger.Print("Exchanged for access token")

	jiraSession.AccessToken = accessToken
	jiraSession.AccessSecret = accessSecret

	_, err = database.GetServiceDB().StoreAuthSession(jiraSession)
	if err != nil {
		failWith(logger, w, 500, "Failed to persist JIRA session", err)
		return
	}
	w.WriteHeader(200)
	w.Write([]byte("OK!"))
}

func (r *jiraRealm) AuthSession(id, userID, realmID string) types.AuthSession {
	return &JIRASession{
		id:      id,
		userID:  userID,
		realmID: realmID,
	}
}

// jiraClient returns an authenticated jira.Client for the given userID. Returns an unauthenticated
// client if allowUnauth is true and no authenticated session is found, else returns an error.
func (r *jiraRealm) jiraClient(jiraBaseURL, userID string, allowUnauth bool) (*jira.Client, error) {
	// TODO: Check if user has an auth session. Requires access token+secret
	hasAuthSession := false

	if hasAuthSession {
		// make an authenticated client
		var cli *jira.Client

		auth := r.oauth1Config(jiraBaseURL)

		httpClient := auth.Client(context.TODO(), oauth1.NewToken("access_tokenTODO", "access_secretTODO"))
		cli, err := jira.NewClient(httpClient, jiraBaseURL)
		return cli, err
	} else if allowUnauth {
		// make an unauthenticated client
		cli, err := jira.NewClient(nil, jiraBaseURL)
		return cli, err
	} else {
		return nil, errors.New("No authenticated session found for " + userID)
	}
}

func (r *jiraRealm) ensureInited() error {
	if err := r.parsePrivateKey(); err != nil {
		log.WithError(err).Print("Failed to parse private key")
		return err
	}
	// Parse the messy input URL into a canonicalised form.
	ju, err := urls.ParseJIRAURL(r.JIRAEndpoint)
	if err != nil {
		log.WithError(err).Print("Failed to parse JIRA endpoint")
		return err
	}
	r.JIRAEndpoint = ju.Base
	return nil
}

func (r *jiraRealm) parsePrivateKey() error {
	if r.privateKey != nil {
		return nil
	}
	pk, err := loadPrivateKey(r.PrivateKeyPEM)
	if err != nil {
		return err
	}
	pub, err := publicKeyAsPEM(pk)
	if err != nil {
		return err
	}
	r.PublicKeyPEM = pub
	r.privateKey = pk
	return nil
}

func (r *jiraRealm) oauth1Config(jiraBaseURL string) *oauth1.Config {
	return &oauth1.Config{
		ConsumerKey:    r.ConsumerKey,
		ConsumerSecret: r.ConsumerSecret,
		CallbackURL:    r.redirectURL,
		// TODO: In JIRA Cloud, the Authorization URL is only the Instance BASE_URL:
		//    https://BASE_URL.atlassian.net.
		// It also does not require the + "/plugins/servlet/oauth/authorize"
		// We should probably check the provided JIRA base URL to see if it is a cloud one
		// then adjust accordingly.
		Endpoint: oauth1.Endpoint{
			RequestTokenURL: jiraBaseURL + "plugins/servlet/oauth/request-token",
			AuthorizeURL:    jiraBaseURL + "plugins/servlet/oauth/authorize",
			AccessTokenURL:  jiraBaseURL + "plugins/servlet/oauth/access-token",
		},
		Signer: &oauth1.RSASigner{
			PrivateKey: r.privateKey,
		},
	}
}

func loadPrivateKey(privKeyPEM string) (*rsa.PrivateKey, error) {
	// Decode PEM to grab the private key type
	block, _ := pem.Decode([]byte(privKeyPEM))
	if block == nil {
		return nil, errors.New("No PEM formatted block found")
	}

	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return priv, nil
}

func publicKeyAsPEM(pkey *rsa.PrivateKey) (string, error) {
	// https://github.com/golang-samples/cipher/blob/master/crypto/rsa_keypair.go
	der, err := x509.MarshalPKIXPublicKey(&pkey.PublicKey)
	if err != nil {
		return "", err
	}
	block := pem.Block{
		Type:    "PUBLIC KEY",
		Headers: nil,
		Bytes:   der,
	}
	return string(pem.EncodeToMemory(&block)), nil
}

// jiraServiceInfo is the HTTP response to JIRA_ENDPOINT/rest/api/2/serverInfo
type jiraServiceInfo struct {
	ServerTitle    string `json:"serverTitle"`
	Version        string `json:"version"`
	VersionNumbers []int  `json:"versionNumbers"`
	BaseURL        string `json:"baseUrl"`
}

func jiraServerInfo(cli *jira.Client) (*jiraServiceInfo, error) {
	var jsi jiraServiceInfo
	req, _ := cli.NewRequest("GET", "rest/api/2/serverInfo", nil)
	if _, err := cli.Do(req, &jsi); err != nil {
		return nil, err
	}
	return &jsi, nil
}

// TODO: Github has this as well, maybe factor it out?
func failWith(logger *log.Entry, w http.ResponseWriter, code int, msg string, err error) {
	logger.WithError(err).Print(msg)
	w.WriteHeader(code)
	w.Write([]byte(msg))
}

func init() {
	types.RegisterAuthRealm(func(realmID, redirectURL string) types.AuthRealm {
		return &jiraRealm{id: realmID, redirectURL: redirectURL}
	})
}
