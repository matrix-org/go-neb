package travisci

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
)

// Host => Public Key.
// Travis has a .com and .org with different public keys.
// .org is the public one and is one we will try first, then .com
var travisPublicKeyMap = map[string]*rsa.PublicKey{
	"api.travis-ci.org": nil,
	"api.travis-ci.com": nil,
}

func verifyOrigin(payload []byte, sigHeader string) error {
	/*
		From: https://docs.travis-ci.com/user/notifications#Verifying-Webhook-requests
			 1. Pick up the payload data from the HTTP request’s body.
			 2. Obtain the Signature header value, and base64-decode it.
			 3. Obtain the public key corresponding to the private key that signed the payload.
				This is available at the /config endpoint’s config.notifications.webhook.public_key on
				the relevant API server. (e.g., https://api.travis-ci.org/config)
			4. Verify the signature using the public key and SHA1 digest.
	*/
	sig, err := base64.StdEncoding.DecodeString(sigHeader)
	if err != nil {
		return fmt.Errorf("verifyOrigin: Failed to decode signature as base64: %s", err)
	}

	if err := loadPublicKeys(); err != nil {
		return fmt.Errorf("verifyOrigin: Failed to cache Travis public keys: %s", err)
	}

	// 4. Verify with SHA1
	// NB: We don't know who sent this request (no Referer header or anything) so we need to try
	//     both public keys at both endpoints. We use the .org one first since it's more popular.
	var verifyErr error
	for _, host := range []string{"api.travis-ci.org", "api.travis-ci.com"} {
		h := sha1.New()
		h.Write(payload)
		digest := h.Sum(nil)
		verifyErr = rsa.VerifyPKCS1v15(travisPublicKeyMap[host], crypto.SHA1, digest, sig)
		if verifyErr == nil {
			return nil // Valid for this key
		}
	}
	return fmt.Errorf("verifyOrigin: Signature verification failed: %s", verifyErr)
}

func loadPublicKeys() error {
	for _, host := range []string{"api.travis-ci.com", "api.travis-ci.org"} {
		pubKey := travisPublicKeyMap[host]
		if pubKey == nil {
			pemPubKey, err := fetchPEMPublicKey("https://" + host + "/config")
			if err != nil {
				return err
			}
			block, _ := pem.Decode([]byte(pemPubKey))
			if block == nil {
				return fmt.Errorf("public_key at %s doesn't have a valid PEM block", host)
			}

			k, err := x509.ParsePKIXPublicKey(block.Bytes)
			if err != nil {
				return err
			}
			pubKey = k.(*rsa.PublicKey)
			travisPublicKeyMap[host] = pubKey
		}
	}
	return nil
}

func fetchPEMPublicKey(travisURL string) (key string, err error) {
	var res *http.Response
	res, err = httpClient.Get(travisURL)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return
	}
	configStruct := struct {
		Config struct {
			Notifications struct {
				Webhook struct {
					PublicKey string `json:"public_key"`
				} `json:"webhook"`
			} `json:"notifications"`
		} `json:"config"`
	}{}
	if err = json.NewDecoder(res.Body).Decode(&configStruct); err != nil {
		return
	}
	key = configStruct.Config.Notifications.Webhook.PublicKey
	if key == "" || !strings.HasPrefix(key, "-----BEGIN PUBLIC KEY-----") {
		err = fmt.Errorf("Couldn't fetch Travis-CI public key. Missing or malformed key: %s", key)
	}
	return
}
