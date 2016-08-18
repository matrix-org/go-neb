package services

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/google/go-github/github"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"github.com/matrix-org/go-neb/realms/github"
	"github.com/matrix-org/go-neb/services/github/client"
	"github.com/matrix-org/go-neb/services/github/webhook"
	"github.com/matrix-org/go-neb/types"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Matches alphanumeric then a /, then more alphanumeric then a #, then a number.
// E.g. owner/repo#11 (issue/PR numbers) - Captured groups for owner/repo/number
var ownerRepoIssueRegex = regexp.MustCompile("([A-z0-9-_]+)/([A-z0-9-_]+)#([0-9]+)")

type githubService struct {
	id                 string
	webhookEndpointURL string
	BotUserID          string
	ClientUserID       string
	RealmID            string
	SecretToken        string
	Rooms              map[string]struct { // room_id => {}
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

func (s *githubService) cmdGithubCreate(roomID, userID string, args []string) (interface{}, error) {
	cli := s.githubClientFor(userID, false)
	if cli == nil {
		r, err := database.GetServiceDB().LoadAuthRealm(s.RealmID)
		if err != nil {
			return nil, err
		}
		ghRealm, ok := r.(*realms.GithubRealm)
		if !ok {
			return nil, fmt.Errorf("Failed to cast realm %s into a GithubRealm", s.RealmID)
		}
		return matrix.StarterLinkMessage{
			Body: "You need to OAuth with Github before you can create issues.",
			Link: ghRealm.StarterLink,
		}, nil
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
}

func (s *githubService) expandIssue(roomID, userID string, matchingGroups []string) interface{} {
	// matchingGroups => ["foo/bar#11", "foo", "bar", "11"]
	if len(matchingGroups) != 4 {
		log.WithField("groups", matchingGroups).Print("Unexpected number of groups")
		return nil
	}
	num, err := strconv.Atoi(matchingGroups[3])
	if err != nil {
		log.WithField("issue_number", matchingGroups[3]).Print("Bad issue number")
		return nil
	}
	owner := matchingGroups[1]
	repo := matchingGroups[2]

	cli := s.githubClientFor(userID, true)

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
}

func (s *githubService) Plugin(roomID string) plugin.Plugin {
	return plugin.Plugin{
		Commands: []plugin.Command{
			plugin.Command{
				Path: []string{"github", "create"},
				Command: func(roomID, userID string, args []string) (interface{}, error) {
					return s.cmdGithubCreate(roomID, userID, args)
				},
			},
		},
		Expansions: []plugin.Expansion{
			plugin.Expansion{
				Regexp: ownerRepoIssueRegex,
				Expand: func(roomID, userID string, matchingGroups []string) interface{} {
					return s.expandIssue(roomID, userID, matchingGroups)
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
		return fmt.Errorf(
			"User %s does not have a Github auth session with realm %s.", s.ClientUserID, realm.ID())
	}

	log.Infof("%+v", s)

	return nil
}

func (s *githubService) PostRegister(oldService types.Service) {
	cli := s.githubClientFor(s.ClientUserID, false)
	if cli == nil {
		log.Errorf("PostRegister: %s does not have a github session", s.ClientUserID)
		return
	}

	if oldService != nil {
		old, ok := oldService.(*githubService)
		if !ok {
			log.Error("PostRegister: Provided old service is not of type GithubService")
			return
		}

		// Don't spam github webhook requests if we can help it.
		if sameRepos(s, old) {
			log.Print("PostRegister: old and new services have the same repo set. Nooping.")
			return
		}

		// TODO: We should be adding webhooks in Register() then removing old hooks in PostRegister()
		//
		// By doing both operations in PostRegister(), if some of the requests fail we can end up in
		// an inconsistent state. It is a lot simpler and easy to reason about this way though, so
		// for now it will do.

		// remove any existing webhooks this service created on the user's behalf
		modifyWebhooks(old, cli, true)
	}

	// make new webhooks according to service config
	modifyWebhooks(s, cli, false)
}

func modifyWebhooks(s *githubService, cli *github.Client, removeHooks bool) {
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
			removeHook(logger, cli, owner, repo, s.webhookEndpointURL)
		} else {
			// make a hook for all GH events since we'll filter it when we receive webhook requests
			name := "web" // https://developer.github.com/v3/repos/hooks/#create-a-hook
			cfg := map[string]interface{}{
				"content_type": "json",
				"url":          s.webhookEndpointURL,
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
			} else {
				logger.WithField("endpoint", s.webhookEndpointURL).Print("Created hook with endpoint")
			}
		}
	}
}

func sameRepos(a *githubService, b *githubService) bool {
	getRepos := func(s *githubService) []string {
		r := make(map[string]bool)
		for _, roomConfig := range s.Rooms {
			for ownerRepo := range roomConfig.Repos {
				r[ownerRepo] = true
			}
		}
		var rs []string
		for k := range r {
			rs = append(rs, k)
		}
		return rs
	}
	aRepos := getRepos(a)
	bRepos := getRepos(b)

	if len(aRepos) != len(bRepos) {
		return false
	}

	sort.Strings(aRepos)
	sort.Strings(bRepos)
	for i := 0; i < len(aRepos); i++ {
		if aRepos[i] != bRepos[i] {
			return false
		}
	}
	return true
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
		return client.New(token)
	} else if allowUnauth {
		return client.New("")
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

func removeHook(logger *log.Entry, cli *github.Client, owner, repo, webhookEndpointURL string) {
	logger.WithField("endpoint", webhookEndpointURL).Print("Removing hook with endpoint")
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
		logger.WithField("endpoint", webhookEndpointURL).Print("Failed to find hook with endpoint")
		return // couldn't find it
	}

	_, err = cli.Repositories.DeleteHook(owner, repo, *hook.ID)
	if err != nil {
		logger.WithError(err).Print("Failed to delete hook")
	}
	logger.WithField("endpoint", webhookEndpointURL).Print("Deleted hook")
}

func init() {
	types.RegisterService(func(serviceID, webhookEndpointURL string) types.Service {
		return &githubService{
			id:                 serviceID,
			webhookEndpointURL: webhookEndpointURL,
		}
	})
}
