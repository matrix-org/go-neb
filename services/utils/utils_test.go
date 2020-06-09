package utils

import (
	"testing"

	mevt "maunium.net/go/mautrix/event"
)

func TestHTMLStrip(t *testing.T) {
	msg := `before &lt;<hello a="b"><inside />during</hello>&gt; after`
	stripped := StrippedHTMLMessage(mevt.MsgNotice, msg)
	if stripped.MsgType != mevt.MsgNotice {
		t.Fatalf("Expected MsgType %v, got %v", mevt.MsgNotice, stripped.MsgType)
	}
	expected := "before <during> after"
	if stripped.Body != expected {
		t.Fatalf(`Expected Body "%v", got "%v"`, expected, stripped.Body)
	}
}
