package travisci

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/testutils"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

const travisOrgPEMPublicKey = (`-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvtjdLkS+FP+0fPC09j25
y/PiuYDDivIT86COVedvlElk99BBYTrqNaJybxjXbIZ1Q6xFNhOY+iTcBr4E1zJu
tizF3Xi0V9tOuP/M8Wn4Y/1lCWbQKlWrNQuqNBmhovF4K3mDCYswVbpgTmp+JQYu
Bm9QMdieZMNry5s6aiMA9aSjDlNyedvSENYo18F+NYg1J0C0JiPYTxheCb4optr1
5xNzFKhAkuGs4XTOA5C7Q06GCKtDNf44s/CVE30KODUxBi0MCKaxiXw/yy55zxX2
/YdGphIyQiA5iO1986ZmZCLLW8udz9uhW5jUr3Jlp9LbmphAC61bVSf4ou2YsJaN
0QIDAQAB
-----END PUBLIC KEY-----`)

const travisComPEMPublicKey = (`-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAnQU2j9lnRtyuW36arNOc
dzCzyKVirLUi3/aLh6UfnTVXzTnx8eHUnBn1ZeQl7Eh3J3qqdbIKl6npS27ONzCy
3PIcfjpLPaVyGagIL8c8XgDEvB45AesC0osVP5gkXQkPUM3B2rrUmp1AZzG+Fuo0
SAeNnS71gN63U3brL9fN/MTCXJJ6TvMt3GrcJUq5uq56qNeJTsiowK6eiiWFUSfh
e1qapOdMFmcEs9J/R1XQ/scxbAnLcWfl8lqH/MjMdCMe0j3X2ZYMTqOHsb3cQGSS
dMPwZGeLWV+OaxjJ7TrJ+riqMANOgqCBGpvWUnUfo046ACOx7p6u4fFc3aRiuqYK
VQIDAQAB
-----END PUBLIC KEY-----`)

const exampleSignature = ("pW0CDpmcAeWw3qf2Ufx8UvzfrZRUpYx30HBl9nJcDkh2v9FrF1GjJVsrcqx7ly0FPjb7dkfMJ/d0Q3JpDb1EL4p509cN4Vy8+HpfINw35Wg6JqzOQqTa" +
	"TidwoDLXo0NgL78zfiL3dra7ZwOGTA+LmnLSuNp38ROxn70u26uqJzWprGSdVNbRu1LkF1QKLa61NZegfxK7RZn1PlIsznWIyTS0qj81mg2sXMDLH1J4" +
	"pHxjEpzydjSb5b8tCjrN+vFLDdAtP5RjU8NwvQM4LRRGbLDIlRsO77HDwfXrPgUE3DjPIqVpHhMcCusygp0ClH2b1J1O3LkhxSS9ol5w99Hkpg==")
const exampleBody = ("payload=%7B%22id%22%3A176075135%2C%22repository%22%3A%7B%22id%22%3A6461770%2C%22name%22%3A%22flow-jsdoc%22%2C%22owner_" +
	"name%22%3A%22Kegsay%22%2C%22url%22%3Anull%7D%2C%22number%22%3A%2218%22%2C%22config%22%3A%7B%22notifications%22%3A%7B%22web" +
	"hooks%22%3A%5B%22http%3A%2F%2F7abbe705.ngrok.io%22%5D%7D%2C%22language%22%3A%22node_js%22%2C%22node_js%22%3A%5B%224.1%22%5D%2C%22.resu" +
	"lt%22%3A%22configured%22%2C%22group%22%3A%22stable%22%2C%22dist%22%3A%22precise%22%7D%2C%22status%22%3A0%2C%22result%22%3A0%2C%22status_" +
	"message%22%3A%22Passed%22%2C%22result_message%22%3A%22Passed%22%2C%22started_at%22%3A%222016-11-15T15%3A10%3A22Z%22%2C%22finished_" +
	"at%22%3A%222016-11-15T15%3A10%3A54Z%22%2C%22duration%22%3A32%2C%22build_url%22%3A%22https%3A%2F%2Ftravis-ci.org%2FKegsay%2Fflow-js" +
	"doc%2Fbuilds%2F176075135%22%2C%22commit_id%22%3A50222535%2C%22commit%22%3A%223a092c3a6032ebb50384c99b445f947e9ce86e2a%22%2C%22base_com" +
	"mit%22%3Anull%2C%22head_commit%22%3Anull%2C%22branch%22%3A%22master%22%2C%22message%22%3A%22Test+Travis+webhook+support%22%2C%22compare_" +
	"url%22%3A%22https%3A%2F%2Fgithub.com%2FKegsay%2Fflow-jsdoc%2Fcompare%2F9f9d459ba082...3a092c3a6032%22%2C%22committed_at%22%3A%222016-1" +
	"1-15T15%3A08%3A16Z%22%2C%22author_name%22%3A%22Kegan+Dougal%22%2C%22author_email%22%3A%22kegan%40matrix.org%22%2C%22committer_" +
	"name%22%3A%22Kegan+Dougal%22%2C%22committer_email%22%3A%22kegan%40matrix.org%22%2C%22matrix%22%3A%5B%7B%22id%22%3A176075137%2C%22reposit" +
	"ory_id%22%3A6461770%2C%22parent_id%22%3A176075135%2C%22number%22%3A%2218.1%22%2C%22state%22%3A%22finished%22%2C%22config%22%3A%7B%22notifi" +
	"cations%22%3A%7B%22webhooks%22%3A%5B%22http%3A%2F%2F7abbe705.ngrok.io%22%5D%7D%2C%22language%22%3A%22node_js%22%2C%22node_" +
	"js%22%3A%224.1%22%2C%22.result%22%3A%22configured%22%2C%22group%22%3A%22stable%22%2C%22dist%22%3A%22precise%22%2C%22os%22%3A%22li" +
	"nux%22%7D%2C%22status%22%3A0%2C%22result%22%3A0%2C%22commit%22%3A%223a092c3a6032ebb50384c99b445f947e9ce86e2a%22%2C%22branch%22%3A%22mas" +
	"ter%22%2C%22message%22%3A%22Test+Travis+webhook+support%22%2C%22compare_url%22%3A%22https%3A%2F%2Fgithub.com%2FKegsay%2Fflow-jsdoc%2Fcomp" +
	"are%2F9f9d459ba082...3a092c3a6032%22%2C%22started_at%22%3A%222016-11-15T15%3A10%3A22Z%22%2C%22finished_at%22%3A%222016-11-" +
	"15T15%3A10%3A54Z%22%2C%22committed_at%22%3A%222016-11-15T15%3A08%3A16Z%22%2C%22author_name%22%3A%22Kegan+Dougal%22%2C%22author_ema" +
	"il%22%3A%22kegan%40matrix.org%22%2C%22committer_name%22%3A%22Kegan+Dougal%22%2C%22committer_email%22%3A%22kegan%40matrix.org%22%2C%22allow_f" +
	"ailure%22%3Afalse%7D%5D%2C%22type%22%3A%22push%22%2C%22state%22%3A%22passed%22%2C%22pull_request%22%3Afalse%2C%22pull_request_number%22%3Anu" +
	"ll%2C%22pull_request_title%22%3Anull%2C%22tag%22%3Anull%7D")

var travisTests = []struct {
	Signature      string
	ValidSignature bool
	Body           string
	Template       string
	ExpectedOutput string
}{
	{
		exampleSignature, true, exampleBody,
		"%{repository}#%{build_number} (%{branch} - %{commit} : %{author}): %{message}",
		"Kegsay/flow-jsdoc#18 (master - 3a092c3a60 : Kegan Dougal): Passed",
	},
	{
		"obviously_invalid_signature", false, exampleBody,
		"%{repository}#%{build_number} (%{branch} - %{commit} : %{author}): %{message}",
		"Kegsay/flow-jsdoc#18 (master - 3a092c3a60 : Kegan Dougal): Passed",
	},
	{
		// Payload is valid but doesn't match signature now
		exampleSignature, false, strings.TrimSuffix(exampleBody, "%7D") + "%2C%22EXTRA_KEY%22%3Anull%7D",
		"%{repository}#%{build_number} (%{branch} - %{commit} : %{author}): %{message}",
		"Kegsay/flow-jsdoc#18 (master - 3a092c3a60 : Kegan Dougal): Passed",
	},
	{
		exampleSignature, true, exampleBody,
		"%{repository}#%{build_number} %{duration}",
		"Kegsay/flow-jsdoc#18 32s",
	},
}

func TestTravisCI(t *testing.T) {
	database.SetServiceDB(&database.NopStorage{})

	// When the service tries to get Travis' public key, return the constant
	urlToKey := make(map[string]string)
	urlToKey["https://api.travis-ci.org/config"] = travisOrgPEMPublicKey
	urlToKey["https://api.travis-ci.com/config"] = travisComPEMPublicKey
	travisTransport := testutils.NewRoundTripper(func(req *http.Request) (*http.Response, error) {
		if key := urlToKey[req.URL.String()]; key != "" {
			escKey, _ := json.Marshal(key)
			return &http.Response{
				StatusCode: 200,
				Body: ioutil.NopCloser(bytes.NewBufferString(
					`{"config":{"notifications":{"webhook":{"public_key":` + string(escKey) + `}}}}`,
				)),
			}, nil
		}
		return nil, fmt.Errorf("Unhandled URL %s", req.URL.String())
	})
	// clobber the http client that the service uses to talk to Travis
	httpClient = &http.Client{Transport: travisTransport}

	// Intercept message sending to Matrix and mock responses
	msgs := []gomatrix.TextMessage{}
	matrixTrans := struct{ testutils.MockTransport }{}
	matrixTrans.RT = func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.String(), "/send/m.room.message") {
			return nil, fmt.Errorf("Unhandled URL: %s", req.URL.String())
		}
		var msg gomatrix.TextMessage
		if err := json.NewDecoder(req.Body).Decode(&msg); err != nil {
			return nil, fmt.Errorf("Failed to decode request JSON: %s", err)
		}
		msgs = append(msgs, msg)
		return &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewBufferString(`{"event_id":"$yup:event"}`)),
		}, nil
	}
	matrixCli, _ := gomatrix.NewClient("https://hyrule", "@travisci:hyrule", "its_a_secret")
	matrixCli.Client = &http.Client{Transport: matrixTrans}

	// BEGIN running the Travis-CI table tests
	// ---------------------------------------
	for _, test := range travisTests {
		msgs = []gomatrix.TextMessage{} // reset sent messages
		mockWriter := httptest.NewRecorder()
		travis := makeService(t, test.Template)
		if travis == nil {
			t.Error("TestTravisCI Failed to create service")
			continue
		}
		if err := travis.Register(nil, matrixCli); err != nil {
			t.Errorf("TestTravisCI Failed to Register(): %s", err)
			continue
		}
		req, err := http.NewRequest(
			"POST", "https://neb.endpoint/travis-ci-service", bytes.NewBufferString(test.Body),
		)
		if err != nil {
			t.Errorf("TestTravisCI Failed to create webhook request: %s", err)
			continue
		}
		req.Header.Set("Signature", test.Signature)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		travis.OnReceiveWebhook(mockWriter, req, matrixCli)

		if test.ValidSignature {
			if !assertResponse(t, mockWriter, msgs, 200, 1) {
				continue
			}

			if msgs[0].Body != test.ExpectedOutput {
				t.Errorf("TestTravisCI want matrix body '%s', got '%s'", test.ExpectedOutput, msgs[0].Body)
			}
		} else {
			assertResponse(t, mockWriter, msgs, 403, 0)
		}
	}
}

func assertResponse(t *testing.T, w *httptest.ResponseRecorder, msgs []gomatrix.TextMessage, expectCode int, expectMsgLength int) bool {
	if w.Code != expectCode {
		t.Errorf("TestTravisCI OnReceiveWebhook want HTTP code %d, got %d", expectCode, w.Code)
		return false
	}
	if len(msgs) != expectMsgLength {
		t.Errorf("TestTravisCI want %d sent messages, got %d ", expectMsgLength, len(msgs))
		return false
	}
	return true
}

func makeService(t *testing.T, template string) *Service {
	srv, err := types.CreateService("id", ServiceType, "@travisci:hyrule", []byte(
		`{
			"rooms":{
				"!ewfug483gsfe:localhost": {
					"repos": {
						"Kegsay/flow-jsdoc": {
							"template": "`+template+`"
						}
					}
				}
			}
		}`,
	))
	if err != nil {
		t.Error("Failed to create Travis-CI service: ", err)
		return nil
	}
	return srv.(*Service)
}
