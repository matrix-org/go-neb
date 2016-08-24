package services

import (
	"database/sql"
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
	"github.com/matrix-org/go-neb/util"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Matches alphanumeric then a /, then more alphanumeric then a #, then a number.
// E.g. owner/repo#11 (issue/PR numbers) - Captured groups for owner/repo/number
var ownerRepoIssueRegex = regexp.MustCompile(`(([A-z0-9-_]+)/([A-z0-9-_]+))?#([0-9]+)`)

type githubService struct {
	id                 string
	serviceUserID      string
	webhookEndpointURL string
	ClientUserID       string // optional; required for webhooks
	RealmID            string
	SecretToken        string
	HandleCommands     bool
	HandleExpansions   bool
	Rooms              map[string]struct { // room_id => {}
		Repos map[string]struct { // owner/repo => { events: ["push","issue","pull_request"] }
			Events []string
		}
	}
}

func (s *githubService) ServiceUserID() string { return s.serviceUserID }
func (s *githubService) ServiceID() string     { return s.id }
func (s *githubService) ServiceType() string   { return "github" }
func (s *githubService) cmdGithubCreate(roomID, userID string, args []string) (interface{}, error) {
	if !s.HandleCommands {
		return nil, nil
	}
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

	// We expect the args to look like:
	// [ "owner/repo", "title text", "desc text" ]
	// They can omit the owner/repo if there is a default one set.

	if len(args) < 2 || strings.Count(args[0], "/") != 1 {
		// look for a default repo
		defaultRepo := s.defaultRepo(roomID)
		if defaultRepo == "" {
			return &matrix.TextMessage{"m.notice",
				`Usage: !github create owner/repo "issue title" "description"`}, nil
		}
		// insert the default as the first arg to reuse the same code path
		args = append([]string{defaultRepo}, args...)
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

func (s *githubService) expandIssue(roomID, userID, owner, repo string, issueNum int) interface{} {
	cli := s.githubClientFor(userID, true)

	i, _, err := cli.Issues.Get(owner, repo, issueNum)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"owner":  owner,
			"repo":   repo,
			"number": issueNum,
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
					if !s.HandleExpansions {
						return nil
					}
					// There's an optional group in the regex so matchingGroups can look like:
					// [foo/bar#55 foo/bar foo bar 55]
					// [#55                        55]
					if len(matchingGroups) != 5 {
						log.WithField("groups", matchingGroups).WithField("len", len(matchingGroups)).Print(
							"Unexpected number of groups",
						)
						return nil
					}
					if matchingGroups[1] == "" && matchingGroups[2] == "" && matchingGroups[3] == "" {
						// issue only match, this only works if there is a default repo
						defaultRepo := s.defaultRepo(roomID)
						if defaultRepo == "" {
							return nil
						}
						segs := strings.Split(defaultRepo, "/")
						if len(segs) != 2 {
							log.WithFields(log.Fields{
								"room_id":      roomID,
								"default_repo": defaultRepo,
							}).Error("Default repo is malformed")
							return nil
						}
						// Fill in the missing fields in matching groups and fall through into ["foo/bar#11", "foo", "bar", "11"]
						matchingGroups = []string{
							defaultRepo + matchingGroups[0],
							defaultRepo,
							segs[0],
							segs[1],
							matchingGroups[4],
						}
					}
					num, err := strconv.Atoi(matchingGroups[4])
					if err != nil {
						log.WithField("issue_number", matchingGroups[4]).Print("Bad issue number")
						return nil
					}
					return s.expandIssue(roomID, userID, matchingGroups[2], matchingGroups[3], num)
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
	logger := log.WithFields(log.Fields{
		"event": evType,
		"repo":  *repo.FullName,
	})
	repoExistsInConfig := false

	for roomID, roomConfig := range s.Rooms {
		for ownerRepo, repoConfig := range roomConfig.Repos {
			if !strings.EqualFold(*repo.FullName, ownerRepo) {
				continue
			}
			repoExistsInConfig = true // even if we don't notify for it.
			notifyRoom := false
			for _, notifyType := range repoConfig.Events {
				if evType == notifyType {
					notifyRoom = true
					break
				}
			}
			if notifyRoom {
				logger.WithFields(log.Fields{
					"msg":     msg,
					"room_id": roomID,
				}).Print("Sending notification to room")
				if _, e := cli.SendMessageEvent(roomID, "m.room.message", msg); e != nil {
					logger.WithError(e).WithField("room_id", roomID).Print(
						"Failed to send notification to room.")
				}
			}
		}
	}

	if !repoExistsInConfig {
		segs := strings.Split(*repo.FullName, "/")
		if len(segs) != 2 {
			logger.Error("Received event with malformed owner/repo.")
			w.WriteHeader(400)
			return
		}
		if err := s.deleteHook(segs[0], segs[1]); err != nil {
			logger.WithError(err).Print("Failed to delete webhook")
		} else {
			logger.Info("Deleted webhook")
		}
	}

	w.WriteHeader(200)
}

// Register will create webhooks for the repos specified in Rooms
//
// The hooks made are a delta between the old service and the current configuration. If all webhooks are made,
// Register() succeeds. If any webhook fails to be created, Register() fails. A delta is used to allow clients to incrementally
// build up the service config without recreating the hooks every time a change is made.
//
// Hooks are deleted when this service receives a webhook event from Github for a repo which has no user configurations.
//
// Hooks can get out of sync if a user manually deletes a hook in the Github UI. In this case, toggling the repo configuration will
// force NEB to recreate the hook.
func (s *githubService) Register(oldService types.Service, client *matrix.Client) error {
	if s.RealmID == "" {
		return fmt.Errorf("RealmID is required")
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

	if s.ClientUserID != "" {
		// In order to register the GH service as a client, you must have authed with GH.
		cli := s.githubClientFor(s.ClientUserID, false)
		if cli == nil {
			return fmt.Errorf(
				"User %s does not have a Github auth session with realm %s.", s.ClientUserID, realm.ID())
		}
		// Make sure they have specified some webhooks (it makes no sense otherwise)
		reposForWebhooks := s.repoList()
		if len(reposForWebhooks) == 0 {
			return fmt.Errorf("No repos for webhooks specified")
		}

		// Fetch the old service list and work out the difference between the two.
		var oldRepos []string
		if oldService != nil {
			old, ok := oldService.(*githubService)
			if !ok {
				log.WithFields(log.Fields{
					"service_id":   oldService.ServiceID(),
					"service_type": oldService.ServiceType(),
				}).Print("Cannot cast old github service to GithubService")
				// non-fatal though, we'll just make the hooks
			} else {
				oldRepos = old.repoList()
			}
		}

		// Add the repos in the new service but not the old service
		newRepos, _ := util.Difference(reposForWebhooks, oldRepos)
		for _, r := range newRepos {
			logger := log.WithField("repo", r)
			err := s.createHook(cli, r)
			if err != nil {
				logger.WithError(err).Error("Failed to create webhook")
				return err
			}
			logger.Info("Created webhook")
		}

		if err := s.joinWebhookRooms(client); err != nil {
			return err
		}
	}

	log.Infof("%+v", s)

	return nil
}

// defaultRepo returns the default repo for the given room, or an empty string.
func (s *githubService) defaultRepo(roomID string) string {
	logger := log.WithFields(log.Fields{
		"room_id":     roomID,
		"bot_user_id": s.serviceUserID,
	})
	opts, err := database.GetServiceDB().LoadBotOptions(s.serviceUserID, roomID)
	if err != nil {
		if err != sql.ErrNoRows {
			logger.WithError(err).Error("Failed to load bot options")
		}
		return ""
	}
	// Expect opts to look like:
	// { github: { default_repo: $OWNER_REPO } }
	ghOpts, ok := opts.Options["github"].(map[string]interface{})
	if !ok {
		logger.WithField("options", opts.Options).Error("Failed to cast bot options as github options")
		return ""
	}
	defaultRepo, ok := ghOpts["default_repo"].(string)
	if !ok {
		logger.WithField("default_repo", ghOpts["default_repo"]).Error(
			"Failed to cast default repo as a string",
		)
		return ""
	}
	return defaultRepo
}

func (s *githubService) joinWebhookRooms(client *matrix.Client) error {
	for roomID := range s.Rooms {
		if _, err := client.JoinRoom(roomID, "", ""); err != nil {
			// TODO: Leave the rooms we successfully joined?
			return err
		}
	}
	return nil
}

func (s *githubService) repoList() []string {
	var repos []string
	if s.Rooms == nil {
		return repos
	}
	for _, roomConfig := range s.Rooms {
		for ownerRepo := range roomConfig.Repos {
			if strings.Count(ownerRepo, "/") != 1 {
				log.WithField("repo", ownerRepo).Error("Bad owner/repo key in config")
				continue
			}
			exists := false
			for _, r := range repos {
				if r == ownerRepo {
					exists = true
					break
				}
			}
			if !exists {
				repos = append(repos, ownerRepo)
			}
		}
	}
	return repos
}

func (s *githubService) createHook(cli *github.Client, ownerRepo string) error {
	o := strings.Split(ownerRepo, "/")
	owner := o[0]
	repo := o[1]
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
	_, res, err := cli.Repositories.CreateHook(owner, repo, &github.Hook{
		Name:   &name,
		Config: cfg,
		Events: events,
	})

	if res.StatusCode == 422 {
		errResponse, ok := err.(*github.ErrorResponse)
		if !ok {
			return err
		}
		for _, ghErr := range errResponse.Errors {
			if strings.Contains(ghErr.Message, "already exists") {
				log.WithField("repo", ownerRepo).Print("422 : Hook already exists")
				return nil
			}
		}
		return err
	}

	return err
}

func (s *githubService) deleteHook(owner, repo string) error {
	logger := log.WithFields(log.Fields{
		"endpoint": s.webhookEndpointURL,
		"repo":     owner + "/" + repo,
	})
	logger.Info("Removing hook")

	cli := s.githubClientFor(s.ClientUserID, false)
	if cli == nil {
		logger.WithField("user_id", s.ClientUserID).Print("Cannot delete webhook: no authenticated client exists for user ID.")
		return fmt.Errorf("no authenticated client exists for user ID")
	}

	// Get a list of webhooks for this owner/repo and find the one which has the
	// same endpoint URL which is what github uses to determine equivalence.
	hooks, _, err := cli.Repositories.ListHooks(owner, repo, nil)
	if err != nil {
		return err
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
		if hookURL == s.webhookEndpointURL {
			hook = h
			break
		}
	}
	if hook == nil {
		return fmt.Errorf("Failed to find hook with endpoint: %s", s.webhookEndpointURL)
	}

	_, err = cli.Repositories.DeleteHook(owner, repo, *hook.ID)
	return err
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

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &githubService{
			id:                 serviceID,
			serviceUserID:      serviceUserID,
			webhookEndpointURL: webhookEndpointURL,
		}
	})
}
