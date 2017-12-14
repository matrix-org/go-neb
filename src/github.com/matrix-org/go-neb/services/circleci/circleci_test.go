package circleci

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
	"net/url"
)

const exampleBody = ("%7B%22payload%22%3A%7B%22vcs_url%22%3A%22https%3A%2F%2Fgithub.com%2Fcircleci%2Fmongofinil%22%2C%22build_url%22%3A%22https%3A%2F%2Fcircleci.com%2Fgh%2Fcircleci%2Fmongofinil%2F22%22%2C%22build_num%22%3A22%2C%22branch%22%3A%22master%22%2C%22vcs_revision%22%3A%221d231626ba1d2838e599c5c598d28e2306ad4e48%22%2C%22committer_name%22%3A%22Allen%20Rohner%22%2C%22committer_email%22%3A%22arohner%40gmail.com%22%2C%22subject%22%3A%22Don%27t%20explode%20when%20the%20system%20clock%20shifts%20backwards%22%2C%22body%22%3A%22%22%2C%22why%22%3A%22github%22%2C%22dont_build%22%3Anull%2C%22queued_at%22%3A%222013-02-12T21%3A33%3A30Z%22%2C%22start_time%22%3A%222013-02-12T21%3A33%3A38Z%22%2C%22stop_time%22%3A%222013-02-12T21%3A34%3A01Z%22%2C%22build_time_millis%22%3A23505%2C%22username%22%3A%22circleci%22%2C%22reponame%22%3A%22mongofinil%22%2C%22lifecycle%22%3A%22finished%22%2C%22outcome%22%3A%22success%22%2C%22status%22%3A%22success%22%2C%22retry_of%22%3Anull%2C%22steps%22%3A%5B%7B%22name%22%3A%22configure%20the%20build%22%2C%22actions%22%3A%5B%7B%22bash_command%22%3Anull%2C%22run_time_millis%22%3A1646%2C%22start_time%22%3A%222013-02-12T21%3A33%3A38Z%22%2C%22end_time%22%3A%222013-02-12T21%3A33%3A39Z%22%2C%22name%22%3A%22configure%20the%20build%22%2C%22exit_code%22%3Anull%2C%22type%22%3A%22infrastructure%22%2C%22index%22%3A0%2C%22status%22%3A%22success%22%7D%5D%7D%2C%7B%22name%22%3A%22lein2%20deps%22%2C%22actions%22%3A%5B%7B%22bash_command%22%3A%22lein2%20deps%22%2C%22run_time_millis%22%3A7555%2C%22start_time%22%3A%222013-02-12T21%3A33%3A47Z%22%2C%22messages%22%3A%5B%5D%2C%22step%22%3A1%2C%22exit_code%22%3A0%2C%22end_time%22%3A%222013-02-12T21%3A33%3A54Z%22%2C%22index%22%3A0%2C%22status%22%3A%22success%22%2C%22type%22%3A%22dependencies%22%2C%22source%22%3A%22inference%22%2C%22failed%22%3Anull%7D%5D%7D%2C%7B%22name%22%3A%22lein2%20trampoline%20midje%22%2C%22actions%22%3A%5B%7B%22bash_command%22%3A%22lein2%20trampoline%20midje%22%2C%22run_time_millis%22%3A2310%2C%22continue%22%3Anull%2C%22parallel%22%3Atrue%2C%22start_time%22%3A%222013-02-12T21%3A33%3A59Z%22%2C%22name%22%3A%22lein2%20trampoline%20midje%22%2C%22messages%22%3A%5B%5D%2C%22step%22%3A6%2C%22exit_code%22%3A1%2C%22end_time%22%3A%222013-02-12T21%3A34%3A01Z%22%2C%22index%22%3A0%2C%22status%22%3A%22failed%22%2C%22timedout%22%3Anull%2C%22infrastructure_fail%22%3Anull%2C%22type%22%3A%22test%22%2C%22source%22%3A%22inference%22%2C%22failed%22%3Atrue%7D%5D%7D%5D%7D%7D")
var circleciTests = []struct {
	Body           string
	Template       string
	ExpectedOutput string
}{
	{
		exampleBody,
		"%{repository_slug}#%{buildnum} (%{branch} - %{commit} : %{committername}): %{outcome}",
		"circleci/mongofinil#22 (master - 1d231626ba : Allen Rohner): success",
	},
	{
		exampleBody,
		"%{repository_slug}#%{buildnum} (%{branch} - %{commit} : %{committername}): %{outcome}",
		"circleci/mongofinil#22 (master - 1d231626ba : Allen Rohner): success",
	},
	{
		strings.TrimSuffix(exampleBody, "%7D") + "%2C%22EXTRA_KEY%22%3Anull%7D",
		"%{repository_slug}#%{buildnum} (%{branch} - %{commit} : %{committername}): %{outcome}",
		"circleci/mongofinil#22 (master - 1d231626ba : Allen Rohner): success",
	},
	{
		exampleBody,
		"%{repository_slug}#%{buildnum} %{duration}",
		"circleci/mongofinil#22 23s",
	},
}

func TestCircleCI(t *testing.T) {
	database.SetServiceDB(&database.NopStorage{})

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
	matrixCli, _ := gomatrix.NewClient("https://hyrule", "@circleci:hyrule", "its_a_secret")
	matrixCli.Client = &http.Client{Transport: matrixTrans}

	// BEGIN running the CircleCI table tests
	// ---------------------------------------
	for _, test := range circleciTests {
		msgs = []gomatrix.TextMessage{} // reset sent messages
		mockWriter := httptest.NewRecorder()
		circleci := makeService(t, test.Template)
		if circleci == nil {
			t.Error("TestCircleCI Failed to create service")
			continue
		}
		if err := circleci.Register(nil, matrixCli); err != nil {
			t.Errorf("TestCircleCI Failed to Register(): %s", err)
			continue
		}
		correctBody, convErr := url.QueryUnescape(test.Body)
		if convErr != nil {
			t.Errorf("TestCircleCI Failed to UnUrlEncode Test Body: %s", convErr)
			continue
		}
		req, err := http.NewRequest(
			"POST", "https://neb.endpoint/circleci-service", bytes.NewBufferString(correctBody),
		)
		if err != nil {
			t.Errorf("TestCircleCI Failed to create webhook request: %s", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		circleci.OnReceiveWebhook(mockWriter, req, matrixCli)

		if !assertResponse(t, mockWriter, msgs, 200, 1) {
			continue
		}

		if msgs[0].Body != test.ExpectedOutput {
			t.Errorf("TestCircleCI want matrix body '%s', got '%s'", test.ExpectedOutput, msgs[0].Body)
		}
	}
}

func assertResponse(t *testing.T, w *httptest.ResponseRecorder, msgs []gomatrix.TextMessage, expectCode int, expectMsgLength int) bool {
	if w.Code != expectCode {
		t.Errorf("TestCircleCI OnReceiveWebhook want HTTP code %d, got %d", expectCode, w.Code)
		return false
	}
	if len(msgs) != expectMsgLength {
		t.Errorf("TestCircleCI want %d sent messages, got %d ", expectMsgLength, len(msgs))
		return false
	}
	return true
}

func makeService(t *testing.T, template string) *Service {
	srv, err := types.CreateService("id", ServiceType, "@circleci:hyrule", []byte(
		`{
			"rooms":{
				"!ewfug483gsfe:localhost": {
					"repos": {
						"circleci/mongofinil": {
							"template": "`+template+`"
						}
					}
				}
			}
		}`,
	))
	if err != nil {
		t.Error("Failed to create CircleCI service: ", err)
		return nil
	}
	return srv.(*Service)
}
