package realms

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/types"
	"net/http"
)

type jiraRealm struct {
	id             string
	ConsumerName   string
	ConsumerKey    string
	ConsumerSecret string
	PrivateKeyPEM  string
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
	// Make sure the private key PEM is actually a private key.
	_, err := loadPrivateKey(r.PrivateKeyPEM)
	if err != nil {
		return err
	}
	return nil
}

func (r *jiraRealm) RequestAuthSession(userID string, req json.RawMessage) interface{} {
	reqAuth := struct {
		JIRAURL string
	}{}
	if err := json.Unmarshal(req, reqAuth); err != nil {
		log.WithError(err).Print("Error parsing request JSON")
		return nil
	}
	if reqAuth.JIRAURL == "" {
		log.Print("Missing JIRAURL")
		return nil
	}
	// TODO: Check to see if JIRA endpoint is valid and known
	return nil
}

func (r *jiraRealm) OnReceiveRedirect(w http.ResponseWriter, req *http.Request) {

}

func (r *jiraRealm) AuthSession(id, userID, realmID string) types.AuthSession {
	return nil
}

func loadPrivateKey(privKeyPEM string) (*rsa.PrivateKey, error) {
	// Decode PEM to grab the private key type
	block, _ := pem.Decode([]byte(privKeyPEM))
	if block == nil {
		return nil, errors.New("No PEM formatted block found")
	}

	// TODO: Handle passwords on private keys.
	// decBytes, err = x509.DecryptPEMBlock(block, []byte{}) // no pass

	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return priv, nil
}

func init() {
	types.RegisterAuthRealm(func(realmID string) types.AuthRealm {
		return &jiraRealm{id: realmID}
	})
}
