package plugin

import (
	"github.com/matrix-org/go-neb/matrix"
	"reflect"
	"regexp"
	"testing"
)

const (
	myRoomID = "!room:example.com"
	mySender = "@user:example.com"
)

func makeTestEvent(msgtype, body string) *matrix.Event {
	return &matrix.Event{
		Sender: mySender,
		Type:   "m.room.message",
		RoomID: myRoomID,
		Content: map[string]interface{}{
			"body":    body,
			"msgtype": msgtype,
		},
	}
}

type testResponse struct {
	RoomID    string
	Arguments []string
}

func makeTestResponse(roomID, sender string, arguments []string) interface{} {
	return testResponse{roomID, arguments}
}

type testExpansion struct {
	RoomID       string
	UserID       string
	MatchingText string
}

func makeTestExpansion(roomID, userID, matchingText string) interface{} {
	return testExpansion{roomID, userID, matchingText}
}

func makeTestPlugin(paths [][]string, regexps []*regexp.Regexp) Plugin {
	var commands []Command
	for _, path := range paths {
		commands = append(commands, Command{
			Path: path,
			Command: func(roomID, sender string, arguments []string) (interface{}, error) {
				return makeTestResponse(roomID, sender, arguments), nil
			},
		})
	}
	var expansions []Expansion
	for _, re := range regexps {
		expansions = append(expansions, Expansion{
			Regexp: re,
			Expand: makeTestExpansion,
		})
	}

	return Plugin{Commands: commands, Expansions: expansions}
}

func TestRunCommands(t *testing.T) {
	plugins := []Plugin{makeTestPlugin([][]string{
		[]string{"test", "command"},
	}, nil)}
	event := makeTestEvent("m.text", `!test command arg1 "arg 2" 'arg 3'`)
	got := runCommands(plugins, event)
	want := []interface{}{makeTestResponse(myRoomID, mySender, []string{
		"arg1", "arg 2", "arg 3",
	})}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runCommands(\nplugins=%+v\nevent=%+v\n)\n%+v\nwanted: %+v", plugins, event, got, want)
	}
}

func TestRunCommandsBestMatch(t *testing.T) {
	plugins := []Plugin{makeTestPlugin([][]string{
		[]string{"test", "command"},
		[]string{"test", "command", "more", "specific"},
	}, nil)}
	event := makeTestEvent("m.text", "!test command more specific arg1")
	got := runCommands(plugins, event)
	want := []interface{}{makeTestResponse(myRoomID, mySender, []string{
		"arg1",
	})}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runCommands(\nplugins=%+v\nevent=%+v\n)\n%+v\nwanted: %+v", plugins, event, got, want)
	}
}

func TestRunCommandsMultiplePlugins(t *testing.T) {
	plugins := []Plugin{
		makeTestPlugin([][]string{[]string{"test", "command", "first"}}, nil),
		makeTestPlugin([][]string{[]string{"test", "command"}}, nil),
	}
	event := makeTestEvent("m.text", "!test command first arg1")
	got := runCommands(plugins, event)
	want := []interface{}{
		makeTestResponse(myRoomID, mySender, []string{"arg1"}),
		makeTestResponse(myRoomID, mySender, []string{"first", "arg1"}),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runCommands(\nplugins=%+v\nevent=%+v\n)\n%+v\nwanted: %+v", plugins, event, got, want)
	}
}

func TestRunCommandsInvalidShell(t *testing.T) {
	plugins := []Plugin{
		makeTestPlugin([][]string{[]string{"test", "command"}}, nil),
	}
	event := makeTestEvent("m.text", `!test command 'mismatched quotes"`)
	got := runCommands(plugins, event)
	want := []interface{}{
		makeTestResponse(myRoomID, mySender, []string{"'mismatched", `quotes"`}),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runCommands(\nplugins=%+v\nevent=%+v\n)\n%+v\nwanted: %+v", plugins, event, got, want)
	}
}

func TestExpansion(t *testing.T) {
	plugins := []Plugin{
		makeTestPlugin(nil, []*regexp.Regexp{
			regexp.MustCompile("a[^ ]*"),
			regexp.MustCompile("b.."),
		}),
	}
	event := makeTestEvent("m.text", "test banana for scale")
	got := runCommands(plugins, event)
	want := []interface{}{
		makeTestExpansion(myRoomID, mySender, "anana"),
		makeTestExpansion(myRoomID, mySender, "ale"),
		makeTestExpansion(myRoomID, mySender, "ban"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runCommands(\nplugins=%+v\nevent=%+v\n)\n%+v\nwanted: %+v", plugins, event, got, want)
	}
}

func TestExpansionDuplicateMatches(t *testing.T) {
	plugins := []Plugin{
		makeTestPlugin(nil, []*regexp.Regexp{
			regexp.MustCompile("badger"),
		}),
	}
	event := makeTestEvent("m.text", "badger badger badger")
	got := runCommands(plugins, event)
	want := []interface{}{
		makeTestExpansion(myRoomID, mySender, "badger"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runCommands(\nplugins=%+v\nevent=%+v\n)\n%+v\nwanted: %+v", plugins, event, got, want)
	}
}
