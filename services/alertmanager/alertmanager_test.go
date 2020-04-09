package alertmanager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/testutils"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestNotify(t *testing.T) {
	database.SetServiceDB(&database.NopStorage{})

	// Intercept message sending to Matrix and mock responses
	msgs := []gomatrix.HTMLMessage{}
	matrixCli := buildTestClient(&msgs)

	// create the service
	srv := buildTestService(t)

	// send a notification
	req, err := http.NewRequest(
		"POST", "", bytes.NewBufferString(`
			{
				"externalURL": "http://alertmanager",
				 "alerts": [
					{
						"labels": {
							"alertname": "alert 1",
							"severity": "huge"
						},
						"generatorURL": "http://x"
					},
					{
						"labels": {
							"alertname": "alert 2",
							"severity": "tiny"
						},
						"generatorURL": "http://y"
					}
				]
			}
		`),
	)
	if err != nil {
		t.Fatalf("Failed to create webhook request: %s", err)
	}
	mockWriter := httptest.NewRecorder()
	srv.OnReceiveWebhook(mockWriter, req, matrixCli)

	// check response
	if mockWriter.Code != 200 {
		t.Fatalf("Expected response 200 OK, got %d", mockWriter.Code)
	}
	if len(msgs) != 1 {
		t.Fatalf("Expected sent 1 msgs, sent %d", len(msgs))
	}
	msg := msgs[0]
	if msg.MsgType != "m.text" {
		t.Errorf("Wrong msgtype: got %s want m.text", msg.MsgType)
	}

	lines := strings.Split(msg.FormattedBody, "\n")

	// <a href="http://alertmanager#silences/new?filter=%7balertname%3D%22alert%202%22,severity%3D%22tiny%22%7d">silence</a>
	matchedSilence := 0
	for _, line := range lines {
		if !strings.Contains(line, "silence") {
			continue
		}

		matchedSilence++
		checkSilenceLine(t, line, map[string]string{
			"alertname": "\"alert 1\"",
			"severity":  "\"huge\"",
		})
		break
	}

	if matchedSilence == 0 {
		t.Errorf("Did not find any silence lines")
	}
}

func buildTestClient(msgs *[]gomatrix.HTMLMessage) *gomatrix.Client {
	matrixTrans := struct{ testutils.MockTransport }{}
	matrixTrans.RT = func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.String(), "/send/m.room.message") {
			return nil, fmt.Errorf("Unhandled URL: %s", req.URL.String())
		}
		var msg gomatrix.HTMLMessage
		if err := json.NewDecoder(req.Body).Decode(&msg); err != nil {
			return nil, fmt.Errorf("Failed to decode request JSON: %s", err)
		}
		*msgs = append(*msgs, msg)
		return &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewBufferString(`{"event_id":"$yup:event"}`)),
		}, nil
	}
	matrixCli, _ := gomatrix.NewClient("https://hs", "@neb:hs", "its_a_secret")
	matrixCli.Client = &http.Client{Transport: matrixTrans}
	return matrixCli
}

func buildTestService(t *testing.T) types.Service {
	htmlTemplate, err := json.Marshal(
		`{{range .Alerts}}
		{{index .Labels "severity" }} : {{- index .Labels "alertname" -}}
		<a href="{{ .GeneratorURL }}">source</a>
		<a href="{{ .SilenceURL }}">silence</a>
		{{- end }}
		`,
	)

	if err != nil {
		t.Fatal(err)
	}

	textTemplate, err := json.Marshal(`{{range .Alerts}}{{index .Labels "alertname"}} {{end}}`)
	if err != nil {
		t.Fatal(err)
	}

	config := fmt.Sprintf(`{
		"rooms":{ "!testroom:id" : {
			"text_template":%s,
			"html_template":%s,
			"msg_type":"m.text"
		}}
	}`, textTemplate, htmlTemplate,
	)

	srv, err := types.CreateService("id", "alertmanager", "@neb:hs", []byte(config))

	if err != nil {
		t.Fatal(err)
	}

	return srv
}

func checkSilenceLine(t *testing.T, line string, expectedKeys map[string]string) {
	silenceRegexp := regexp.MustCompile(`<a href="http://alertmanager#silences/new\?filter=%7b([^"]*)%7d">silence</a>`)
	m := silenceRegexp.FindStringSubmatch(line)
	if m == nil {
		t.Errorf("silence line %s had bad format", line)
		return
	}

	unesc, err := url.QueryUnescape(m[1])
	if err != nil {
		t.Errorf("Unable to decode filter, %v", err)
		return
	}

	matched := 0
	for _, f := range strings.Split(unesc, ",") {
		splits := strings.SplitN(f, "=", 2)
		key := splits[0]
		exp, ok := expectedKeys[key]
		if !ok {
			t.Errorf("unexpected key in filter: %v", key)
		} else if exp != splits[1] {
			t.Errorf("bad value for filter key %v: got %q, want %q", key, splits[1], exp)
		} else {
			matched++
		}
	}

	if matched != len(expectedKeys) {
		t.Errorf("number of filter fields got %d, want %d", matched, len(expectedKeys))
	}
}
