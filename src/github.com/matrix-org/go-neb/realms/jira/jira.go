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

type JIRASession struct {
	id      string // request token
	userID  string
	realmID string
	Secret  string // request secret
}

func (s *JIRASession) UserID() string {
	return s.userID
}

func (s *JIRASession) RealmID() string {
	return s.realmID
}

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

	// Make sure the private key PEM is actually a private key.
	err := r.parsePrivateKey()
	if err != nil {
		return err
	}

	// Parse the messy input URL into a canonicalised form.
	ju, err := urls.ParseJIRAURL(r.JIRAEndpoint)
	if err != nil {
		return err
	}
	r.JIRAEndpoint = ju.Base

	// Check to see if JIRA endpoint is valid by pinging an endpoint
	cli, err := r.jiraClient(ju, "", true)
	if err != nil {
		return err
	}
	info, err := jiraServerInfo(cli)
	if err != nil {
		return err
	}
	log.WithFields(log.Fields{
		"jira_url": ju.Base,
		"title":    info.ServerTitle,
		"version":  info.Version,
	}).Print("Found JIRA endpoint")
	r.Server = info.ServerTitle
	r.Version = info.Version

	return nil
}

func (r *jiraRealm) RequestAuthSession(userID string, req json.RawMessage) interface{} {
	logger := log.WithField("jira_url", r.JIRAEndpoint)
	// Parse the private key as we may not have called Register()
	err := r.parsePrivateKey()
	if err != nil {
		logger.WithError(err).Print("Failed to parse private key")
		return nil
	}
	ju, err := urls.ParseJIRAURL(r.JIRAEndpoint)
	if err != nil {
		log.WithError(err).Print("Failed to parse JIRA endpoint")
		return nil
	}
	authConfig := r.oauth1Config(ju)
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
		id:      reqToken,
		userID:  userID,
		realmID: r.id,
		Secret:  reqSec,
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

}

func (r *jiraRealm) AuthSession(id, userID, realmID string) types.AuthSession {
	return nil
}

// jiraClient returns an authenticated jira.Client for the given userID. Returns an unauthenticated
// client if allowUnauth is true and no authenticated session is found, else returns an error.
func (r *jiraRealm) jiraClient(u urls.JIRAURL, userID string, allowUnauth bool) (*jira.Client, error) {
	// TODO: Check if user has an auth session. Requires access token+secret
	hasAuthSession := false

	if hasAuthSession {
		// make an authenticated client
		var cli *jira.Client

		auth := r.oauth1Config(u)

		httpClient := auth.Client(context.TODO(), oauth1.NewToken("access_tokenTODO", "access_secretTODO"))
		cli, err := jira.NewClient(httpClient, u.Base)
		return cli, err
	} else if allowUnauth {
		// make an unauthenticated client
		cli, err := jira.NewClient(nil, u.Base)
		return cli, err
	} else {
		return nil, errors.New("No authenticated session found for " + userID)
	}
}

func (r *jiraRealm) parsePrivateKey() error {
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

func (r *jiraRealm) oauth1Config(u urls.JIRAURL) *oauth1.Config {
	return &oauth1.Config{
		ConsumerKey:    r.ConsumerKey,
		ConsumerSecret: r.ConsumerSecret,
		// TODO: path from goneb.go - we should factor it out like we did with Services
		CallbackURL: u.Base + "realms/redirect/" + r.id,
		// TODO: In JIRA Cloud, the Authorization URL is only the Instance BASE_URL:
		//    https://BASE_URL.atlassian.net.
		// It also does not require the + "/plugins/servlet/oauth/authorize"
		// We should probably check the provided JIRA base URL to see if it is a cloud one
		// then adjust accordingly.
		Endpoint: oauth1.Endpoint{
			RequestTokenURL: u.Base + "plugins/servlet/oauth/request-token",
			AuthorizeURL:    u.Base + "plugins/servlet/oauth/authorize",
			AccessTokenURL:  u.Base + "plugins/servlet/oauth/access-token",
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
	_, err := cli.Do(req, &jsi)
	if err != nil {
		return nil, err
	}
	return &jsi, nil
}

func init() {
	types.RegisterAuthRealm(func(realmID string) types.AuthRealm {
		return &jiraRealm{id: realmID}
	})
}
