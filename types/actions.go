package types

import (
	"regexp"
	"strings"
)

// A Command is something that a user invokes by sending a message starting with '!'
// followed by a list of strings that name the command, followed by a list of argument
// strings. The argument strings may be quoted using '\"' and '\'' in the same way
// that they are quoted in the unix shell.
type Command struct {
	Path      []string
	Arguments []string
	Help      string
	Command   func(roomID, userID string, arguments []string) (content interface{}, err error)
}

// An Expansion is something that actives when the user sends any message
// containing a string matching a given pattern. For example an RFC expansion
// might expand "RFC 6214" into "Adaptation of RFC 1149 for IPv6" and link to
// the appropriate RFC.
type Expansion struct {
	Regexp *regexp.Regexp
	Expand func(roomID, userID string, matchingGroups []string) interface{}
}

// Matches if the arguments start with the path of the command.
func (command *Command) Matches(arguments []string) bool {
	if len(arguments) < len(command.Path) {
		return false
	}
	for i, segment := range command.Path {
		if strings.ToLower(segment) != strings.ToLower(arguments[i]) {
			return false
		}
	}
	return true
}
