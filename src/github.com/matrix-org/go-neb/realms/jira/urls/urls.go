// Package urls handles converting between various JIRA URL representations in a consistent way. There exists three main
// types of JIRA URL which Go-NEB cares about:
//    - URL Keys => matrix.org/jira
//    - Base URLs => https://matrix.org/jira/
//    - REST URLs => https://matrix.org/jira/rest/api/2/issue/12680
// When making outbound requests to JIRA, Go-NEB needs to use the Base URL representation. Likewise, when Go-NEB
// sends Matrix messages with JIRA URLs in them, the Base URL needs to be used to form the URL. The URL Key is
// used to determine equivalence of various JIRA installations and is mainly required when searching the database.
// The REST URLs are present on incoming webhook events and are the only way to map the event to a JIRA installation.
package urls

import (
	"errors"
	"net/url"
	"strings"
)

// JIRAURL contains the parsed representation of a JIRA URL
type JIRAURL struct {
	Base string // The base URL of the JIRA installation. Always has a trailing / and a protocol.
	Key  string // The URL key of the JIRA installation. Never has a trailing / or a protocol.
	Raw  string // The raw input URL, if given. Freeform.
}

// ParseJIRAURL parses a raw input URL and returns a struct which has various JIRA URL representations. The input
// URL can be a JIRA REST URL, a speculative base JIRA URL from a client, or a URL key. The input string will be
// stored as under JIRAURL.Raw. If a URL key is given, this struct will default to https as the protocol.
func ParseJIRAURL(u string) (j JIRAURL, err error) {
	if u == "" {
		err = errors.New("No input JIRA URL")
		return
	}
	j.Raw = u
	// URL keys don't have a protocol, everything else does
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		// assume input is a URL key
		k, e := makeURLKey(u)
		if e != nil {
			err = e
			return
		}
		j.Key = k
		j.Base = makeBaseURL(u)
		return
	}
	// Attempt to parse out REST API paths. This is a horrible heuristic which mostly works.
	if strings.Contains(u, "/rest/api/") {
		j.Base = makeBaseURL(strings.Split(u, "/rest/api/")[0])
	} else {
		// Assume it already is a base URL
		j.Base = makeBaseURL(u)
	}

	k, e := makeURLKey(j.Base)
	if e != nil {
		err = e
		return
	}
	j.Key = k
	return
}

// SameJIRAURL returns true if the two given JIRA URLs are pointing to the same JIRA installation.
// Equivalence is determined solely by the provided URLs, by sanitising them then comparing.
func SameJIRAURL(a, b string) bool {
	ja, err := ParseJIRAURL(a)
	if err != nil {
		return false
	}
	jb, err := ParseJIRAURL(b)
	if err != nil {
		return false
	}
	return ja.Key == jb.Key
}

// makeBaseURL assumes the input is a base URL and makes sure that the string conforms to JIRA Base URL rules:
//  - Must have a protocol
//  - Must have a trailing slash
// Defaults to HTTPS if there is no protocol specified.
func makeBaseURL(s string) string {
	if !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "http://") {
		s = "https://" + s
	}
	return withTrailingSlash(s)
}

// makeURLKey assumes the input is a URL key and makes sure that the string conforms to JIRA URL Key rules:
//  - Must not have a protocol
//  - Must not have a trailing slash
// For example:
//   https://matrix.org/jira/  =>  matrix.org/jira
func makeURLKey(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	return u.Host + strings.TrimSuffix(u.Path, "/"), nil
}

// withTrailingSlash makes sure the input string has a trailing slash. Will not add one if one already exists.
func withTrailingSlash(s string) string {
	if strings.HasSuffix(s, "/") {
		return s
	}
	return s + "/"
}
