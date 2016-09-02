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
	"github.com/matrix-org/go-neb/types"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Matches alphanumeric then a /, then more alphanumeric then a #, then a number.
// E.g. owner/repo#11 (issue/PR numbers) - Captured groups for owner/repo/number
var ownerRepoIssueRegex = regexp.MustCompile(`(([A-z0-9-_]+)/([A-z0-9-_]+))?#([0-9]+)`)

type githubService struct {
	id            string
	serviceUserID string
	RealmID       string
}

func (s *githubService) ServiceUserID() string { return s.serviceUserID }
func (s *githubService) ServiceID() string     { return s.id }
func (s *githubService) ServiceType() string   { return "github" }
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

func (s *githubService) Plugin(cli *matrix.Client, roomID string) plugin.Plugin {
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
	w.WriteHeader(400)
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

	log.Infof("%+v", s)
	return nil
}

func (s *githubService) PostRegister(oldService types.Service) {}

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
			id:            serviceID,
			serviceUserID: serviceUserID,
		}
	})
}
