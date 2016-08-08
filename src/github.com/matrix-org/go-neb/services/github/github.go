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
	id             string
	BotUserID      string
	ClientUserID   string
	RealmID        string
	SecretToken    string
	WebhookBaseURI string
	Rooms          map[string]struct { // room_id => {}
		Repos map[string]struct { // owner/repo => { events: ["push","issue","pull_request"] }
			Events []string
		}
	}
}

func (s *githubService) ServiceUserID() string { return s.BotUserID }
func (s *githubService) ServiceID() string     { return s.id }
func (s *githubService) ServiceType() string   { return "github" }
func (s *githubService) RoomIDs() []string {
	var keys []string
	for k := range s.Rooms {
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

					if len(args) < 2 {
						return &matrix.TextMessage{"m.notice",
							`Usage: !github create owner/repo "issue title" "description"`}, nil
					}

					var (
						ownerRepo string
						title     *string
						desc      *string
					)
					ownerRepo = args[0]
					o := strings.Split(ownerRepo, "/")
					if len(o) != 2 {
						return &matrix.TextMessage{"m.notice",
							`Usage: !github create owner/repo "issue title" "description"`}, nil
					}

					if len(args) == 2 {
						title = &args[1]
					} else if len(args) == 3 {
						title = &args[1]
						desc = &args[2]
					} else { // > 3 args is probably a title without quote marks
						joinedTitle := strings.Join(args[1:], " ")
						title = &joinedTitle
					}

					issue, res, err := cli.Issues.Create(o[0], o[1], &github.IssueRequest{
						Title: title,
						Body:  desc,
					})
					if err != nil {
						log.WithField("err", err).Print("Failed to create issue")
						return nil, fmt.Errorf("Failed to create issue. HTTP %d", res.StatusCode)
					}

					return matrix.TextMessage{"m.notice", fmt.Sprintf("Created issue: %s", *issue.HTMLURL)}, nil
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
	evType, repo, msg, err := webhook.OnReceiveRequest(req, s.SecretToken)
	if err != nil {
		w.WriteHeader(err.Code)
		return
	}

	for roomID, roomConfig := range s.Rooms {
		for ownerRepo, repoConfig := range roomConfig.Repos {
			if !strings.EqualFold(*repo.FullName, ownerRepo) {
				continue
			}

			notifyRoom := false
			for _, notifyType := range repoConfig.Events {
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
	}
	w.WriteHeader(200)
}
func (s *githubService) Register() error {
	if s.RealmID == "" || s.ClientUserID == "" || s.BotUserID == "" {
		return fmt.Errorf("RealmID, BotUserID and ClientUserID are required")
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

	// In order to register the GH service, you must have authed with GH.
	cli := s.githubClientFor(s.ClientUserID, false)
	if cli == nil {
		return fmt.Errorf("User %s does not have a Github auth session.", s.ClientUserID)
	}

	return nil
}

func (s *githubService) PostRegister(oldService types.Service) {
	cli := s.githubClientFor(s.ClientUserID, false)
	if cli == nil {
		log.Errorf("PostRegister: %s does not have a github session", s.ClientUserID)
		return
	}
	old, ok := oldService.(*githubService)
	if !ok {
		log.Error("PostRegister: Provided old service is not of type GithubService")
		return
	}

	// TODO: We should be adding webhooks in Register() then removing old hooks in PostRegister()
	//
	// By doing both operations in PostRegister(), if some of the requests fail we can end up in
	// an inconsistent state. It is a lot simpler and easy to reason about this way though, so
	// for now it will do.

	// remove any existing webhooks this service created on the user's behalf
	modifyWebhooks(old, cli, true)

	// make new webhooks according to service config
	modifyWebhooks(s, cli, false)
}

func modifyWebhooks(s *githubService, cli *github.Client, removeHooks bool) {
	// TODO: This makes assumptions about how Go-NEB maps services to webhook endpoints.
	//       We should factor this out to a function called GetWebhookEndpoint(Service) or something.
	trailingSlash := ""
	if !strings.HasSuffix(s.WebhookBaseURI, "/") {
		trailingSlash = "/"
	}
	webhookEndpointURL := s.WebhookBaseURI + trailingSlash + "services/hooks/" + s.id

	ownerRepoSet := make(map[string]bool)
	for _, roomCfg := range s.Rooms {
		for ownerRepo := range roomCfg.Repos {
			// sanity check that it looks like 'owner/repo' as we'll split on / later
			if strings.Count(ownerRepo, "/") != 1 {
				log.WithField("owner_repo", ownerRepo).Print("Bad owner/repo value.")
				continue
			}
			ownerRepoSet[ownerRepo] = true
		}
	}

	for ownerRepo := range ownerRepoSet {
		o := strings.Split(ownerRepo, "/")
		owner := o[0]
		repo := o[1]
		logger := log.WithFields(log.Fields{
			"owner": owner,
			"repo":  repo,
		})
		if removeHooks {
			removeHook(logger, cli, owner, repo, webhookEndpointURL)
		} else {
			// make a hook for all GH events since we'll filter it when we receive webhook requests
			name := "web" // https://developer.github.com/v3/repos/hooks/#create-a-hook
			cfg := map[string]interface{}{
				"content_type": "json",
				"url":          webhookEndpointURL,
			}
			if s.SecretToken != "" {
				cfg["secret"] = s.SecretToken
			}
			events := []string{"push", "pull_request", "issues", "issue_comment", "pull_request_review_comment"}
			_, _, err := cli.Repositories.CreateHook(owner, repo, &github.Hook{
				Name:   &name,
				Config: cfg,
				Events: events,
			})
			if err != nil {
				logger.WithError(err).Print("Failed to create webhook")
				// continue as others may succeed
			}
		}
	}
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
	if ghSession.AccessToken == "" {
		return "", fmt.Errorf("Github auth session for %s has not been completed.", userID)
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

func removeHook(logger *log.Entry, cli *github.Client, owner, repo, webhookEndpointURL string) {
	// Get a list of webhooks for this owner/repo and find the one which has the
	// same endpoint URL which is what github uses to determine equivalence.
	hooks, _, err := cli.Repositories.ListHooks(owner, repo, nil)
	if err != nil {
		logger.WithError(err).Print("Failed to list hooks")
		return
	}
	var hook *github.Hook
	for _, h := range hooks {
		if h.Config["url"] == nil {
			logger.Print("Ignoring nil config.url")
			continue
		}
		hookURL, ok := h.Config["url"].(string)
		if !ok {
			logger.Print("Ignoring non-string config.url")
			continue
		}
		if hookURL == webhookEndpointURL {
			hook = h
			break
		}
	}
	if hook == nil {
		return // couldn't find it
	}

	_, err = cli.Repositories.DeleteHook(owner, repo, *hook.ID)
	if err != nil {
		logger.WithError(err).Print("Failed to delete hook")
	}
}

func init() {
	types.RegisterService(func(serviceID string) types.Service {
		return &githubService{id: serviceID}
	})
}
