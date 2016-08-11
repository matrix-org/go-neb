package plugin

import (
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/mattn/go-shellwords"
	"regexp"
	"strings"
)

// A Plugin is a list of commands and expansions to apply to incoming messages.
type Plugin struct {
	Commands   []Command
	Expansions []Expansion
}

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
	Expand func(roomID, userID, matchingText string) interface{}
}

// matches if the arguments start with the path of the command.
func (command *Command) matches(arguments []string) bool {
	if len(arguments) < len(command.Path) {
		return false
	}
	for i, segment := range command.Path {
		if segment != arguments[i] {
			return false
		}
	}
	return true
}

// runCommandForPlugin runs a single command read from a matrix event. Runs
// the matching command with the longest path. Returns the JSON encodable
// content of a single matrix message event to use as a response or nil if no
// response is appropriate.
func runCommandForPlugin(plugin Plugin, event *matrix.Event, arguments []string) interface{} {
	var bestMatch *Command
	for _, command := range plugin.Commands {
		matches := command.matches(arguments)
		betterMatch := bestMatch == nil || len(bestMatch.Path) < len(command.Path)
		if matches && betterMatch {
			bestMatch = &command
		}
	}

	if bestMatch == nil {
		return nil
	}

	cmdArgs := arguments[len(bestMatch.Path):]
	log.WithFields(log.Fields{
		"room_id": event.RoomID,
		"user_id": event.Sender,
		"command": bestMatch.Path,
	}).Info("Executing command")
	content, err := bestMatch.Command(event.RoomID, event.Sender, cmdArgs)
	if err != nil {
		if content != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: err,
				"room_id":    event.RoomID,
				"user_id":    event.Sender,
				"command":    bestMatch.Path,
				"args":       cmdArgs,
			}).Warn("Command returned both error and content.")
		}
		content = matrix.TextMessage{"m.notice", err.Error()}
	}

	return content
}

// run the expansions for a matrix event.
func runExpansionsForPlugin(plugin Plugin, event *matrix.Event, body string) []interface{} {
	var responses []interface{}

	for _, expansion := range plugin.Expansions {
		matches := map[string]bool{}
		for _, matchingText := range expansion.Regexp.FindAllString(body, -1) {
			if matches[matchingText] {
				// Only expand the first occurance of a matching string
				continue
			}
			matches[matchingText] = true
			if response := expansion.Expand(event.RoomID, event.Sender, matchingText); response != nil {
				responses = append(responses, response)
			}
		}
	}

	return responses
}

// runCommands runs the plugin commands or expansions for a single matrix
// event. Returns a list of JSON encodable contents for the matrix messages
// to use as responses.
// If the message beings with '!' then it is assumed to be a command. Each
// plugin is checked for a matching command, if a match is found then that
// command is run. If more than one plugin has a matching command then all
// of those commands are run. This shouldn't happen unless the same plugin
// is installed multiple times since each plugin will usually have a
// distinct prefix for its commands.
// If the message doesn't begin with '!' then it is checked against the
// expansions for each plugin.
func runCommands(plugins []Plugin, event *matrix.Event) []interface{} {
	body, ok := event.Body()
	if !ok || body == "" {
		return nil
	}

	// filter m.notice to prevent loops
	if msgtype, ok := event.MessageType(); !ok || msgtype == "m.notice" {
		return nil
	}

	var responses []interface{}

	if body[0] == '!' {
		args, err := shellwords.Parse(body[1:])
		if err != nil {
			args = strings.Split(body[1:], " ")
		}

		for _, plugin := range plugins {
			if response := runCommandForPlugin(plugin, event, args); response != nil {
				responses = append(responses, response)
			}
		}
	} else {
		for _, plugin := range plugins {
			expansions := runExpansionsForPlugin(plugin, event, body)
			responses = append(responses, expansions...)
		}
	}

	return responses
}

// OnMessage checks the message event to see whether it contains any commands
// or expansions from the listed plugins and processes those commands or
// expansions.
func OnMessage(plugins []Plugin, client *matrix.Client, event *matrix.Event) {
	responses := runCommands(plugins, event)

	for _, content := range responses {
		_, err := client.SendMessageEvent(event.RoomID, "m.room.message", content)
		if err != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: err,
				"room_id":    event.RoomID,
				"user_id":    event.Sender,
				"content":    content,
			}).Print("Failed to send command response")
		}
	}
}
