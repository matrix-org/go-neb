package clients

import (
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
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

func (s *MockService) Commands(cli types.MatrixClient) []types.Command {
	return s.commands
}

type MockStore struct {
	database.NopStorage
	service types.Service
}

func (d *MockStore) LoadServicesForUser(userID id.UserID) ([]types.Service, error) {
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
			Command: func(roomID id.RoomID, userID id.UserID, args []string) (interface{}, error) {
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
	mxCli, _ := mautrix.NewClient("https://someplace.somewhere", "@service:user", "token")
	mxCli.Client = cli
	botClient := BotClient{Client: mxCli}

	for _, input := range commandParseTests {
		executedCmdArgs = []string{}
		content := mevt.Content{Raw: map[string]interface{}{
			"body":    input.body,
			"msgtype": "m.text",
		}}
		if veryRaw, err := content.MarshalJSON(); err != nil {
			t.Errorf("Error marshalling JSON: %s", err)
		} else {
			content.VeryRaw = veryRaw
		}
		content.ParseRaw(mevt.EventMessage)
		event := mevt.Event{
			Type:    mevt.EventMessage,
			Sender:  "@someone:somewhere",
			RoomID:  "!foo:bar",
			Content: content,
		}
		clients.onMessageEvent(&botClient, &event)
		if !reflect.DeepEqual(executedCmdArgs, input.expectArgs) {
			t.Errorf("TestCommandParsing want %s, got %s", input.expectArgs, executedCmdArgs)
		}
	}

}

func TestSASVerificationHandling(t *testing.T) {
	botClient := BotClient{verificationSAS: &sync.Map{}}
	botClient.olmMachine = &crypto.OlmMachine{
		DefaultSASTimeout: time.Minute,
	}
	otherUserID := id.UserID("otherUser")
	otherDeviceID := id.DeviceID("otherDevice")
	otherDevice := &crypto.DeviceIdentity{
		UserID:   otherUserID,
		DeviceID: otherDeviceID,
	}
	botClient.SubmitDecimalSAS(otherUserID, otherDeviceID, crypto.DecimalSASData([3]uint{4, 5, 6}))
	matched := botClient.VerifySASMatch(otherDevice, crypto.DecimalSASData([3]uint{1, 2, 3}))
	if matched {
		t.Error("SAS matched when they shouldn't have")
	}

	botClient.SubmitDecimalSAS(otherUserID, otherDeviceID, crypto.DecimalSASData([3]uint{1, 2, 3}))
	matched = botClient.VerifySASMatch(otherDevice, crypto.DecimalSASData([3]uint{1, 2, 3}))
	if !matched {
		t.Error("Expected SAS to match but they didn't")
	}

	botClient.SubmitDecimalSAS(otherUserID+"wrong", otherDeviceID, crypto.DecimalSASData([3]uint{4, 5, 6}))
	finished := make(chan bool)
	go func() {
		matched := botClient.VerifySASMatch(otherDevice, crypto.DecimalSASData([3]uint{1, 2, 3}))
		finished <- true
		if !matched {
			t.Error("SAS didn't match when it should have (receiving SAS after calling verification func)")
		}
	}()
	select {
	case <-finished:
		t.Error("Verification finished before receiving the SAS from the correct user")
	default:
	}
	botClient.SubmitDecimalSAS(otherUserID, otherDeviceID, crypto.DecimalSASData([3]uint{1, 2, 3}))
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Error("Verification did not finish after receiving the SAS from the correct user")
	}
}
