package urls

import (
	"testing"
)

var urltests = []struct {
	in      string
	outBase string
	outKey  string
	outRaw  string
}{
	// valid url key as input
	{"matrix.org/jira", "https://matrix.org/jira/", "matrix.org/jira", "matrix.org/jira"},
	// valid url base as input
	{"https://matrix.org/jira/", "https://matrix.org/jira/", "matrix.org/jira", "https://matrix.org/jira/"},
	// valid rest url as input
	{"https://matrix.org/jira/rest/api/2/issue/12680", "https://matrix.org/jira/", "matrix.org/jira", "https://matrix.org/jira/rest/api/2/issue/12680"},
	// missing trailing slash as input
	{"https://matrix.org/jira", "https://matrix.org/jira/", "matrix.org/jira", "https://matrix.org/jira"},
	// missing protocol but with trailing slash
	{"matrix.org/jira/", "https://matrix.org/jira/", "matrix.org/jira", "matrix.org/jira/"},
	// no jira path as base url (subdomain)
	{"https://jira.matrix.org", "https://jira.matrix.org/", "jira.matrix.org", "https://jira.matrix.org"},
	// explicit http as input
	{"http://matrix.org/jira", "http://matrix.org/jira/", "matrix.org/jira", "http://matrix.org/jira"},
}

func TestParseJIRAURL(t *testing.T) {
	for _, urltest := range urltests {
		jURL, err := ParseJIRAURL(urltest.in)
		if err != nil {
			t.Fatal(err)
		}
		if jURL.Key != urltest.outKey {
			t.Fatalf("ParseJIRAURL(%s) => Key: Want %s got %s", urltest.in, urltest.outKey, jURL.Key)
		}
		if jURL.Base != urltest.outBase {
			t.Fatalf("ParseJIRAURL(%s) => Base: Want %s got %s", urltest.in, urltest.outBase, jURL.Base)
		}
		if jURL.Raw != urltest.outRaw {
			t.Fatalf("ParseJIRAURL(%s) => Raw: Want %s got %s", urltest.in, urltest.outRaw, jURL.Raw)
		}
	}
}
