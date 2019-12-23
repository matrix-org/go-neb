package alertmanager

import (
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/testutils"
	"github.com/matrix-org/gomatrix"
	"net/http"
	"strings"
	"fmt"
	"encoding/json"
	"io/ioutil"
	"bytes"
	"testing"
	"github.com/matrix-org/go-neb/types"
	"net/http/httptest"
	"regexp"
)

func TestNotify(t *testing.T) {
	database.SetServiceDB(&database.NopStorage{})

	// Intercept message sending to Matrix and mock responses
	msgs := []gomatrix.HTMLMessage{}
	matrixTrans := struct{ testutils.MockTransport }{}
	matrixTrans.RT = func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.String(), "/send/m.room.message") {
			return nil, fmt.Errorf("Unhandled URL: %s", req.URL.String())
		}
		var msg gomatrix.HTMLMessage
		if err := json.NewDecoder(req.Body).Decode(&msg); err != nil {
			return nil, fmt.Errorf("Failed to decode request JSON: %s", err)
		}
		msgs = append(msgs, msg)
		return &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewBufferString(`{"event_id":"$yup:event"}`)),
		}, nil
	}
	matrixCli, _ := gomatrix.NewClient("https://hs", "@neb:hs", "its_a_secret")
	matrixCli.Client = &http.Client{Transport: matrixTrans}

	// create the service
	html_template, err := json.Marshal(
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

    text_template, err := json.Marshal(`{{range .Alerts}}{{index .Labels "alertname"}} {{end}}`)
	if err != nil {
		t.Fatal(err)
	}

    config := fmt.Sprintf(`{
		"rooms":{ "!testroom:id" : {
			"text_template":%s,
			"html_template":%s,
			"msg_type":"m.text"
		}}
	}`, text_template, html_template,
	)

	srv, err := types.CreateService("id", "alertmanager", "@neb:hs", []byte(config))
	if err != nil {
		t.Fatal(err)
	}

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

	lines := strings.Split(msg.FormattedBody, "\n" )

	// 	<a href="http://alertmanager#silences/new?filter=%7balertname%3D%22alert%202%22,severity%3D%22tiny%22%7d">silence</a>
	silenceRegexp := regexp.MustCompile(`<a href="([^"]*)">silence</a>`)
	matchedSilence := 0
	for _, line := range lines {
		if ! strings.Contains(line, "silence") {
			continue
		}

		matchedSilence += 1
		m := silenceRegexp.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("silence line %s had bad format", line)
		} else {
			url := m[1]
			expected := "http://alertmanager#silences/new?filter=%7balertname%3D%22alert%201%22,severity%3D%22huge%22%7d"
			if url != expected {
				t.Errorf("silence url: got %s, want %s", url, expected)
			}
		}
		break
	}

	if matchedSilence == 0 {
		t.Errorf("Did not find any silence lines")
	}
}
