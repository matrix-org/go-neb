package slackapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"mime"
	"net/http"
	"regexp"
	"time"

	"github.com/matrix-org/gomatrix"
	"github.com/russross/blackfriday"
	log "github.com/sirupsen/logrus"
)

type slackAttachment struct {
	Fallback         string `json:"fallback"`
	FallbackRendered template.HTML
	Color            *string `json:"color"`
	ColorRendered    template.HTMLAttr
	Pretext          string `json:"pretext"`
	PretextRendered  template.HTML

	AuthorName    *string      `json:"author_name"`
	AuthorLink    template.URL `json:"author_link"`
	AuthorIcon    *string      `json:"author_icon"`
	AuthorIconURL template.URL

	Title     *string `json:"title"`
	TitleLink *string `json:"title_link"`

	Text         string `json:"text"`
	TextRendered template.HTML

	MrkdwnIn []string `json:"mrkdwn_in"`
	Ts       *int64   `json:"ts"`
}

type slackMessage struct {
	Text         string `json:"text"`
	TextRendered template.HTML
	Username     string            `json:"username"`
	Channel      string            `json:"channel"`
	Mrkdwn       *bool             `json:"mrkdwn"`
	Attachments  []slackAttachment `json:"attachments"`
}

// We use text.template because any fields of any attachments could
// be Markdown, so it's convenient to escape on a field-by field basis.
// We do not do this yet, since it's assumed that clients also escape the content we send them.
var htmlTemplate, _ = template.New("htmlTemplate").Parse(`
<strong>@{{ .Username }}</strong> via <strong>#{{ .Channel }}</strong><br />
{{- with (or .TextRendered .Text nil) }}
	{{- if . }}
		{{- . }}<br />
	{{- end }}
{{- end }}
{{- range .Attachments }}
		{{- if .AuthorName }}
			{{- if .AuthorLink }}<a href="{{ .AuthorLink }}">{{ end }}
				{{- if .AuthorIconUrl }}<img src="{{ .AuthorIconUrl }}" />{{ end }}
				{{- .AuthorName }}
			{{- if .AuthorLink }}</a>{{ end }}
			<br />
		{{- end }}
	<strong>
		<font color="{{- .ColorRendered }}">▌</font>
		{{- if .TitleLink }}
			<a href="{{ .TitleLink}}">{{ .Title }}</a>
		{{- else }}
			{{- .Title }}
		{{- end }}
		<br />
	</strong>
	{{- if .Pretext }}{{ or .PretextRendered .Pretext }}<br />{{ end }}
	{{- if .Text }}{{ or .TextRendered .Text }}<br />{{ end }}
{{- end }}
`)

var netClient = &http.Client{
	Timeout: time.Second * 10,
}

// TODO: What does this do?
var linkRegex, _ = regexp.Compile("<([^|]+)(\\|([^>]+))?>")

func getSlackMessage(req http.Request) (message slackMessage, err error) {
	ct := req.Header.Get("Content-Type")
	ct, _, err = mime.ParseMediaType(ct)

	if ct == "application/x-www-form-urlencoded" {
		req.ParseForm()
		payload := req.Form.Get("payload")
		err = json.Unmarshal([]byte(payload), &message)
	} else if ct == "application/json" {
		decoder := json.NewDecoder(req.Body)
		err = decoder.Decode(&message)
	} else {
		message.Text = fmt.Sprintf("**Error:** unknown Content-Type `%s`", ct)
		log.Error(message.Text)
	}

	return
}

func linkifyString(text string) string {
	return linkRegex.ReplaceAllString(text, "<a href=\"$1\">$3</a>")
}

// Convert a Slack colour (defined at https://api.slack.com/docs/message-attachments )
// into an HTML color.
func getColor(color *string) string {
	if color == nil {
		return "black"
	}

	mappedColor, ok := map[string]string{
		"good":    "green",
		"warning": "yellow",
		"danger":  "red",
	}[*color]
	if ok {
		return mappedColor
	}

	// HTML color= attributes support any arbitrary string, so just pass through.
	return *color
}

// fetches an image and encodes it as a data URL
// returns an empty string if fetch fails
func fetchAndEncodeImage(url *string) (data template.URL) {
	if url == nil {
		return
	}

	var resp *http.Response
	resp, err := netClient.Get(*url)
	if err != nil {
		log.WithError(err).WithField("url", url).Error("Failed to GET URL")
		return
	}

	var (
		body        []byte
		contentType string
	)

	if body, err = ioutil.ReadAll(resp.Body); err != nil {
		return
	}
	if contentType, _, err = mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil {
		return
	}
	base64Body := base64.StdEncoding.EncodeToString(body)
	data = template.URL(fmt.Sprintf("data:%s;base64,%s", contentType, base64Body))

	return
}

func renderSlackAttachment(attachment *slackAttachment) {
	if attachment == nil {
		return
	}

	attachment.ColorRendered = template.HTMLAttr(getColor(attachment.Color))
	attachment.AuthorIconURL = fetchAndEncodeImage(attachment.AuthorIcon)

	for _, fieldName := range attachment.MrkdwnIn {
		var (
			srcField    *string
			targetField *template.HTML
		)

		switch fieldName {
		case "text":
			srcField = &attachment.Text
			targetField = &attachment.TextRendered
		case "pretext":
			srcField = &attachment.Pretext
			targetField = &attachment.PretextRendered
		case "fallback":
			srcField = &attachment.Fallback
			targetField = &attachment.FallbackRendered
		}

		if targetField != nil && srcField != nil {
			*targetField = template.HTML(
				blackfriday.MarkdownBasic([]byte(linkifyString(*srcField))))
		}
	}
}

func slackMessageToHTMLMessage(message slackMessage) (html gomatrix.HTMLMessage, err error) {
	text := linkifyString(message.Text)
	if message.Mrkdwn == nil || *message.Mrkdwn == true {
		message.TextRendered = template.HTML(blackfriday.MarkdownBasic([]byte(text)))
	}

	for attachmentID := range message.Attachments {
		renderSlackAttachment(&message.Attachments[attachmentID])
	}

	var buffer bytes.Buffer
	html.MsgType = "m.text"
	html.Format = "org.matrix.custom.html"
	html.Body, _ = slackMessageToMarkdown(message)
	err = htmlTemplate.ExecuteTemplate(&buffer, "htmlTemplate", message)
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
