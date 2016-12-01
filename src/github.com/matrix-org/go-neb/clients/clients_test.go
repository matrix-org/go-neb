package clients

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

var commandParseTests = []struct {
	body       string
	expectArgs []string
}{
	{"!test word", []string{"word"}},
	{"!test two words", []string{"two", "words"}},
	{`!test "words with double quotes"`, []string{"words with double quotes"}},
	{"!test 'words with single quotes'", []string{"words with single quotes"}},
	{`!test 'single quotes' "double quotes"`, []string{"single quotes", "double quotes"}},
	{`!test ‘smart single quotes’ “smart double quotes”`, []string{"smart single quotes", "smart double quotes"}},
}

type MockService struct {
	types.DefaultService
	commands []types.Command
}

func (s *MockService) Commands(cli *gomatrix.Client) []types.Command {
	return s.commands
}

type MockStore struct {
	database.NopStorage
	service types.Service
}

func (d *MockStore) LoadServicesForUser(userID string) ([]types.Service, error) {
	return []types.Service{d.service}, nil
}

type MockTransport struct {
	roundTrip func(*http.Request) (*http.Response, error)
}

func (t MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.roundTrip(req)
}

func TestCommandParsing(t *testing.T) {
	var executedCmdArgs []string
	cmds := []types.Command{
		types.Command{
			Path: []string{"test"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				executedCmdArgs = args
				return nil, nil
			},
		},
	}
	s := MockService{commands: cmds}
	store := MockStore{service: &s}
	database.SetServiceDB(&store)

	trans := struct{ MockTransport }{}
	trans.roundTrip = func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("unhandled test path")
	}
	cli := &http.Client{
		Transport: trans,
	}
	clients := New(&store, cli)
	mxCli, _ := gomatrix.NewClient("https://someplace.somewhere", "@service:user", "token")
	mxCli.Client = cli

	for _, input := range commandParseTests {
		executedCmdArgs = []string{}
		event := gomatrix.Event{
			Type:   "m.room.message",
			Sender: "@someone:somewhere",
			RoomID: "!foo:bar",
			Content: map[string]interface{}{
				"body":    input.body,
				"msgtype": "m.text",
			},
		}
		clients.onMessageEvent(mxCli, &event)
		if !reflect.DeepEqual(executedCmdArgs, input.expectArgs) {
			t.Errorf("TestCommandParsing want %s, got %s", input.expectArgs, executedCmdArgs)
		}
	}

}
