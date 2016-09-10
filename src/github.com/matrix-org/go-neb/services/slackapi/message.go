package slackapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/russross/blackfriday"
	"io/ioutil"
	"mime"
	"net/http"
	"regexp"
	"text/template"
	"time"
)

type slackAttachment struct {
	Fallback string  `json:"fallback"`
	Color    *string `json:"color"`
	Pretext  string  `json:"pretext"`

	AuthorName *string `json:"author_name"`
	AuthorLink *string `json:"author_link"`
	AuthorIcon *string `json:"author_icon"`

	Title     *string `json:"title"`
	TitleLink *string `json:"title_link"`

	Text     string   `json:"text"`
	MrkdwnIn []string `json:"mrkdwn_in"`
	Ts       *int64   `json:"ts"`
}

type slackMessage struct {
	Text        string            `json:"text"`
	Username    string            `json:"username"`
	Channel     string            `json:"channel"`
	Mrkdwn      *bool             `json:"mrkdwn"`
	Attachments []slackAttachment `json:"attachments"`
}

// We use text.template because any fields of any attachments could
// be Markdown, so it's convenient to escape on a field-by field basis.
// We do not do this yet, since it's assumed that clients also escape the content we send them.
var htmlTemplate, _ = template.New("htmlTemplate").Parse(`
<strong>@{{ .Username }}</strong> via <strong>#{{ .Channel }}</strong><br />
{{ if .Text }}{{ .Text }}<br />{{ end }}
{{ range .Attachments }}
		{{ if .AuthorName }}
			{{if .AuthorLink }}<a href="{{ .AuthorLink }}">{{ end }}
				{{ if .AuthorIcon }}{{ .AuthorIcon }}{{ end }}
				{{ .AuthorName }}
			{{if .AuthorLink }}</a>{{ end }}
			<br />
		{{ end }}
	<strong><font color="{{ .Color }}">▌</font>{{ if .TitleLink }}<a href="{{ .TitleLink}}">{{ .Title }}</a>{{ else }}{{ .Title }}{{ end }}<br /></strong>
	{{ if .Pretext }}{{ .Pretext }}<br />{{ end }}
	{{ if .Text }}{{ .Text }}<br />{{ end }}
{{ end }}
`)

var netClient = &http.Client{
	Timeout: time.Second * 10,
}

var linkRegex, _ = regexp.Compile("<([^|]+)(\\|([^>]+))?>")

func getSlackMessage(req http.Request) (message slackMessage, err error) {
	ct := req.Header.Get("Content-Type")
	ct, _, err = mime.ParseMediaType(ct)

	if ct == "application/x-www-form-urlencoded" {
		req.ParseForm()
		payload := req.Form.Get("payload")
		err = json.Unmarshal([]byte(payload), &message)
	} else if ct == "application/json" {
		log.Info("Parsing as JSON")
		decoder := json.NewDecoder(req.Body)
		err = decoder.Decode(&message)
	} else {
		message.Text = fmt.Sprint("**Error:** unknown Content-Type `%s`", ct)
		log.Error(message.Text)
	}

	return
}

func linkifyString(text string) string {
	return linkRegex.ReplaceAllString(text, "<a href=\"$1\">$3</a>")
}

func getColor(color *string) string {
	if color != nil {
		// https://api.slack.com/docs/message-attachments defines these aliases
		mappedColor, ok := map[string]string{
			"good":    "green",
			"warning": "yellow",
			"danger":  "red",
		}[*color]
		if ok {
			return mappedColor
		}
		return *color
	}
	return "black"
}

func slackMessageToHTMLMessage(message slackMessage) (html matrix.HTMLMessage, err error) {
	processedMessage := message

	if message.Mrkdwn == nil || *message.Mrkdwn == true {
		text := linkifyString(message.Text)

		processedMessage.Text = string(blackfriday.MarkdownBasic([]byte(text)))
	}

	for attachmentID, attachment := range message.Attachments {
		target := &processedMessage.Attachments[attachmentID]

		color := getColor(attachment.Color)
		target.Color = &color

		if attachment.AuthorIcon != nil {
			var resp *http.Response
			resp, err = netClient.Get(*attachment.AuthorIcon)
			if err == nil {
				body, _ := ioutil.ReadAll(resp.Body)
				ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
				b64body := base64.StdEncoding.EncodeToString(body)
				*target.AuthorIcon = fmt.Sprintf("<img src=\"data:%s;base64,%s\" />", ct, b64body)
			} else {
				*target.AuthorIcon = ""
			}
		}

		for _, fieldName := range attachment.MrkdwnIn {
			var targetField, srcField *string

			switch fieldName {
			case "text":
				srcField = &attachment.Text
				targetField = &target.Text
				break
			case "pretext":
				srcField = &attachment.Pretext
				targetField = &target.Pretext
				break
			}

			if targetField != nil && srcField != nil {
				value := string(
					blackfriday.MarkdownBasic([]byte(linkifyString(*srcField))))
				targetField = &value
			}
		}
	}

	var buffer bytes.Buffer
	html.MsgType = "m.text"
	html.Format = "org.matrix.custom.html"
	html.Body, _ = slackMessageToMarkdown(message)
	err = htmlTemplate.ExecuteTemplate(&buffer, "htmlTemplate", processedMessage)
	html.FormattedBody = buffer.String()
	return
}

// This can be improved; Markdown does support all of Slack's formatting
// Which we're just throwing away at the moment.
func slackMessageToMarkdown(message slackMessage) (markdown string, err error) {
	markdown += message.Text + "\n"
	for _, attachment := range message.Attachments {
		markdown += attachment.Fallback + "\n"
	}
	return
}
