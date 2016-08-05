package services

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/google/go-github/github"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"github.com/matrix-org/go-neb/realms/github"
	"github.com/matrix-org/go-neb/services/github/webhook"
	"github.com/matrix-org/go-neb/types"
	"golang.org/x/oauth2"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Matches alphanumeric then a /, then more alphanumeric then a #, then a number.
// E.g. owner/repo#11 (issue/PR numbers) - Captured groups for owner/repo/number
var ownerRepoIssueRegex = regexp.MustCompile("([A-z0-9-_]+)/([A-z0-9-_]+)#([0-9]+)")

type githubService struct {
	id           string
	BotUserID    string
	GithubUserID string
	RealmID      string
	WebhookRooms map[string][]string // room_id => ["push","issue","pull_request"]
}

func (s *githubService) ServiceUserID() string { return s.BotUserID }
func (s *githubService) ServiceID() string     { return s.id }
func (s *githubService) ServiceType() string   { return "github" }
func (s *githubService) RoomIDs() []string {
	var keys []string
	for k := range s.WebhookRooms {
		keys = append(keys, k)
	}
	return keys
}
func (s *githubService) Plugin(roomID string) plugin.Plugin {
	return plugin.Plugin{
		Commands: []plugin.Command{
			plugin.Command{
				Path: []string{"github", "create"},
				Command: func(roomID, userID string, args []string) (interface{}, error) {
					cli := s.githubClientFor(userID, false)
					if cli == nil {
						// TODO: send starter link
						return &matrix.TextMessage{"m.notice",
							userID + " : You have not linked your Github account."}, nil
					}
					return &matrix.TextMessage{"m.notice", strings.Join(args, " ")}, nil
				},
			},
		},
		Expansions: []plugin.Expansion{
			plugin.Expansion{
				Regexp: ownerRepoIssueRegex,
				Expand: func(roomID, userID, matchingText string) interface{} {
					cli := s.githubClientFor(userID, true)
					owner, repo, num, err := ownerRepoNumberFromText(matchingText)
					if err != nil {
						log.WithError(err).WithField("text", matchingText).Print(
							"Failed to extract owner,repo,number from matched string")
						return nil
					}

					i, _, err := cli.Issues.Get(owner, repo, num)
					if err != nil {
						log.WithError(err).WithFields(log.Fields{
							"owner":  owner,
							"repo":   repo,
							"number": num,
						}).Print("Failed to fetch issue")
						return nil
					}

					return &matrix.TextMessage{
						"m.notice",
						fmt.Sprintf("%s : %s", *i.HTMLURL, *i.Title),
					}
				},
			},
		},
	}
}
func (s *githubService) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client) {
	evType, repo, msg, err := webhook.OnReceiveRequest(req, "")
	if err != nil {
		w.WriteHeader(err.Code)
		return
	}

	for roomID, notif := range s.WebhookRooms {
		notifyRoom := false
		for _, notifyType := range notif {
			if evType == notifyType {
				notifyRoom = true
				break
			}
		}
		if notifyRoom {
			log.WithFields(log.Fields{
				"type":    evType,
				"msg":     msg,
				"repo":    repo,
				"room_id": roomID,
			}).Print("Sending notification to room")
			_, e := cli.SendMessageEvent(roomID, "m.room.message", msg)
			if e != nil {
				log.WithError(e).WithField("room_id", roomID).Print(
					"Failed to send notification to room.")
			}
		}
	}
	w.WriteHeader(200)
}
func (s *githubService) Register() error {
	if s.RealmID == "" || s.BotUserID == "" {
		return fmt.Errorf("RealmID and BotUserID are required")
	}
	// check realm exists
	realm, err := database.GetServiceDB().LoadAuthRealm(s.RealmID)
	if err != nil {
		return err
	}
	// make sure the realm is of the type we expect
	if realm.Type() != "github" {
		return fmt.Errorf("Realm is of type '%s', not 'github'", realm.Type())
	}
	return nil
}

func (s *githubService) githubClientFor(userID string, allowUnauth bool) *github.Client {
	token, err := getTokenForUser(s.RealmID, userID)
	if err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey: err,
			"user_id":    userID,
			"realm_id":   s.RealmID,
		}).Print("Failed to get token for user")
	}
	if token != "" {
		return githubClient(token)
	} else if allowUnauth {
		return githubClient("")
	} else {
		return nil
	}
}

func getTokenForUser(realmID, userID string) (string, error) {
	realm, err := database.GetServiceDB().LoadAuthRealm(realmID)
	if err != nil {
		return "", err
	}
	if realm.Type() != "github" {
		return "", fmt.Errorf("Bad realm type: %s", realm.Type())
	}

	// pull out the token (TODO: should the service know how the realm stores this?)
	session, err := database.GetServiceDB().LoadAuthSessionByUser(realm.ID(), userID)
	if err != nil {
		return "", err
	}
	ghSession, ok := session.(*realms.GithubSession)
	if !ok {
		return "", fmt.Errorf("Session is not a github session: %s", session.ID())
	}
	return ghSession.AccessToken, nil
}

// githubClient returns a github Client which can perform Github API operations.
// If `token` is empty, a non-authenticated client will be created. This should be
// used sparingly where possible as you only get 60 requests/hour like that (IP locked).
func githubClient(token string) *github.Client {
	var tokenSource oauth2.TokenSource
	if token != "" {
		tokenSource = oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
	}
	httpCli := oauth2.NewClient(oauth2.NoContext, tokenSource)
	return github.NewClient(httpCli)
}

// ownerRepoNumberFromText parses a GH issue string that looks like 'owner/repo#11'
// into its constituient parts. Returns: owner, repo, issue#.
func ownerRepoNumberFromText(ownerRepoNumberText string) (string, string, int, error) {
	// [full_string, owner, repo, issue_number]
	groups := ownerRepoIssueRegex.FindStringSubmatch(ownerRepoNumberText)
	if len(groups) != 4 {
		return "", "", 0, fmt.Errorf("No match found for '%s'", ownerRepoNumberText)
	}
	num, err := strconv.Atoi(groups[3])
	if err != nil {
		return "", "", 0, err
	}
	return groups[1], groups[2], num, nil
}

func init() {
	types.RegisterService(func(serviceID string) types.Service {
		return &githubService{id: serviceID}
	})
}
