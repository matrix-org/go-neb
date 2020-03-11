// Package jira implements OAuth1.0a support for arbitrary JIRA installations.
package jira

import (
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"

	jira "github.com/andygrunwald/go-jira"
	"github.com/dghubble/oauth1"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/realms/jira/urls"
	"github.com/matrix-org/go-neb/types"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
)

// RealmType of the JIRA realm
const RealmType = "jira"

// Realm is an AuthRealm which can process JIRA installations.
//
// Example request:
//   {
//        "JIRAEndpoint": "matrix.org/jira/",
//        "ConsumerName": "goneb",
//        "ConsumerKey": "goneb",
//        "ConsumerSecret": "random_long_string",
//        "PrivateKeyPEM": "-----BEGIN RSA PRIVATE KEY-----\r\nMIIEowIBAAKCAQEA39UhbOvQHEkBP9fGnhU+eSObTAwX9req2l1NiuNaPU9rE7tf6Bk\r\n-----END RSA PRIVATE KEY-----"
//   }
type Realm struct {
	id          string
	redirectURL string
	privateKey  *rsa.PrivateKey

	// The HTTPS URL of the JIRA installation to authenticate with.
	JIRAEndpoint string
	// The desired "Consumer Name" field of the "Application Links" admin page on JIRA.
	// Generally this is the name of the service. Users will need to enter this string
	// into their JIRA admin web form.
	ConsumerName string
	// The desired "Consumer Key" field of the "Application Links" admin page on JIRA.
	// Generally this is the name of the service. Users will need to enter this string
	// into their JIRA admin web form.
	ConsumerKey string
	// The desired "Consumer Secret" field of the "Application Links" admin page on JIRA.
	// This should be a random long string. Users will need to enter this string into
	// their JIRA admin web form.
	ConsumerSecret string
	// A string which contains the private key for performing OAuth 1.0 requests.
	// This MUST be in PEM format. It must NOT have a password. Go-NEB will convert this
	// into a public key in PEM format and return this to users. Users will need to enter
	// the *public* key into their JIRA admin web form.
	//
	// To generate a private key PEM: (JIRA does not support bit lengths >2048):
	//    $ openssl genrsa -out privkey.pem 2048
	//    $ cat privkey.pem
	PrivateKeyPEM string
	// Optional. If supplied, !jira commands will return this link whenever someone is
	// prompted to login to JIRA.
	StarterLink string

	// The server name of the JIRA installation from /serverInfo.
	// This is an informational field populated by Go-NEB post-creation.
	Server string
	// The JIRA version string from /serverInfo.
	// This is an informational field populated by Go-NEB post-creation.
	Version string
	// The public key for the given private key. This is populated by Go-NEB.
	PublicKeyPEM string

	// Internal field. True if this realm has already registered a webhook with the JIRA installation.
	HasWebhook bool
}

// Session represents a single authentication session between a user and a JIRA endpoint.
// The endpoint is dictated by the realm ID.
type Session struct {
	id      string // request token
	userID  string
	realmID string

	// Configuration fields

	// The secret obtained when requesting an authentication session with JIRA.
	RequestSecret string
	// A JIRA access token for a Matrix user ID.
	AccessToken string
	// A JIRA access secret for a Matrix user ID.
	AccessSecret string
	// Optional. The URL to redirect the client to after authentication.
	ClientsRedirectURL string
}

// AuthRequest is a request for authenticating with JIRA
type AuthRequest struct {
	// Optional. The URL to redirect to after authentication.
	RedirectURL string
}

// AuthResponse is a response to an AuthRequest.
type AuthResponse struct {
	// The URL to visit to perform OAuth on this JIRA installation.
	URL string
}

// Authenticated returns true if the user has completed the auth process
func (s *Session) Authenticated() bool {
	return s.AccessToken != "" && s.AccessSecret != ""
}

// Info returns nothing
func (s *Session) Info() interface{} {
	return nil
}

// UserID returns the ID of the user performing the authentication.
func (s *Session) UserID() string {
	return s.userID
}

// RealmID returns the JIRA realm ID which created this session.
func (s *Session) RealmID() string {
	return s.realmID
}

// ID returns the OAuth1 request_token which is used when looking up sessions in the redirect
// handler.
func (s *Session) ID() string {
	return s.id
}

// ID returns the ID of this JIRA realm.
func (r *Realm) ID() string {
	return r.id
}

// Type returns the type of realm this is.
func (r *Realm) Type() string {
	return RealmType
}

// Init initialises the private key for this JIRA realm.
func (r *Realm) Init() error {
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

// Register is called when this realm is being created from an external entity
func (r *Realm) Register() error {
	if r.ConsumerName == "" || r.ConsumerKey == "" || r.ConsumerSecret == "" || r.PrivateKeyPEM == "" {
		return errors.New("ConsumerName, ConsumerKey, ConsumerSecret, PrivateKeyPEM must be specified")
	}
	if r.JIRAEndpoint == "" {
		return errors.New("JIRAEndpoint must be specified")
	}
	r.HasWebhook = false // never let the user set this; only NEB can.

	// Check to see if JIRA endpoint is valid by pinging an endpoint
	cli, err := r.JIRAClient("", true)
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

// RequestAuthSession is called by a user wishing to auth with this JIRA realm.
// The request body is of type "jira.AuthRequest". Returns a "jira.AuthResponse".
//
// Request example:
//   {
//       "RedirectURL": "https://somewhere.somehow"
//   }
// Response example:
//   {
//       "URL": "https://jira.somewhere.com/plugins/servlet/oauth/authorize?oauth_token=7yeuierbgweguiegrTbOT"
//   }
func (r *Realm) RequestAuthSession(userID string, req json.RawMessage) interface{} {
	logger := log.WithField("jira_url", r.JIRAEndpoint)

	// check if they supplied a redirect URL
	var reqBody AuthRequest
	if err := json.Unmarshal(req, &reqBody); err != nil {
		log.WithError(err).Print("Failed to decode request body")
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

	_, err = database.GetServiceDB().StoreAuthSession(&Session{
		id:                 reqToken,
		userID:             userID,
		realmID:            r.id,
		RequestSecret:      reqSec,
		ClientsRedirectURL: reqBody.RedirectURL,
	})
	if err != nil {
		log.WithError(err).Print("Failed to store new auth session")
		return nil
	}

	return &AuthResponse{authURL.String()}
}

// OnReceiveRedirect is called when JIRA installations redirect back to NEB
func (r *Realm) OnReceiveRedirect(w http.ResponseWriter, req *http.Request) {
	logger := log.WithField("jira_url", r.JIRAEndpoint)

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
	jiraSession, ok := session.(*Session)
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
	if jiraSession.ClientsRedirectURL != "" {
		w.WriteHeader(302)
		w.Header().Set("Location", jiraSession.ClientsRedirectURL)
		// technically don't need a body but *shrug*
		w.Write([]byte(jiraSession.ClientsRedirectURL))
	} else {
		w.WriteHeader(200)
		w.Write([]byte(
			fmt.Sprintf("You have successfully linked your JIRA account on %s to %s",
				r.JIRAEndpoint, jiraSession.UserID(),
			),
		))
	}
}

// AuthSession returns a JIRASession with the given parameters
func (r *Realm) AuthSession(id, userID, realmID string) types.AuthSession {
	return &Session{
		id:      id,
		userID:  userID,
		realmID: realmID,
	}
}

// ProjectKeyExists returns true if the given project key exists on this JIRA realm.
// An authenticated client for userID will be used if one exists, else an
// unauthenticated client will be used, which may not be able to see the complete list
// of projects.
func (r *Realm) ProjectKeyExists(userID, projectKey string) (bool, error) {
	cli, err := r.JIRAClient(userID, true)
	if err != nil {
		return false, err
	}
	var projects []jira.Project
	req, err := cli.NewRequest("GET", "rest/api/2/project", nil)
	if err != nil {
		return false, err
	}
	res, err := cli.Do(req, &projects)
	if err != nil {
		return false, err
	}
	if res == nil {
		return false, errors.New("No response returned")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return false, fmt.Errorf(
			"%srest/api/2/project returned code %d",
			r.JIRAEndpoint, res.StatusCode,
		)
	}

	for _, p := range projects {
		if strings.EqualFold(p.Key, projectKey) {
			return true, nil
		}
	}
	return false, nil
}

// JIRAClient returns an authenticated jira.Client for the given userID. Returns an unauthenticated
// client if allowUnauth is true and no authenticated session is found, else returns an error.
func (r *Realm) JIRAClient(userID string, allowUnauth bool) (*jira.Client, error) {
	// Check if user has an auth session.
	session, err := database.GetServiceDB().LoadAuthSessionByUser(r.id, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			if allowUnauth {
				// make an unauthenticated client
				return jira.NewClient(nil, r.JIRAEndpoint)
			}
		}
		return nil, err
	}

	jsession, ok := session.(*Session)
	if !ok {
		return nil, errors.New("Failed to cast user session to a Session")
	}
	// Make sure they finished the auth process
	if jsession.AccessSecret == "" || jsession.AccessToken == "" {
		if allowUnauth {
			// make an unauthenticated client
			return jira.NewClient(nil, r.JIRAEndpoint)
		}
		return nil, errors.New("No authenticated session found for " + userID)
	}
	// make an authenticated client
	auth := r.oauth1Config(r.JIRAEndpoint)
	httpClient := auth.Client(
		context.TODO(),
		oauth1.NewToken(jsession.AccessToken, jsession.AccessSecret),
	)
	return jira.NewClient(httpClient, r.JIRAEndpoint)
}

func (r *Realm) parsePrivateKey() error {
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

func (r *Realm) oauth1Config(jiraBaseURL string) *oauth1.Config {
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
		return &Realm{id: realmID, redirectURL: redirectURL}
	})
}
