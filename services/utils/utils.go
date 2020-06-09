package utils

import (
	"html"
	"regexp"

	mevt "maunium.net/go/mautrix/event"
)

var htmlRegex = regexp.MustCompile("<[^<]+?>")

// StrippedHTMLMessage returns a MessageEventContent with the body set to a stripped version of the provided HTML,
// in addition to the provided HTML.
func StrippedHTMLMessage(msgtype mevt.MessageType, htmlText string) mevt.MessageEventContent {
	return mevt.MessageEventContent{
		Body:          html.UnescapeString(htmlRegex.ReplaceAllLiteralString(htmlText, "")),
		MsgType:       msgtype,
		Format:        mevt.FormatHTML,
		FormattedBody: htmlText,
	}
}
