// Package github implements a command service and a webhook service for interacting with Github.
//
// The command service is a service which adds !commands and issue expansions for Github. The
// webhook service adds Github webhook support.
package github

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
	gogithub "github.com/google/go-github/github"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/realms/github"
	"github.com/matrix-org/go-neb/services/github/client"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

// ServiceType of the Github service
const ServiceType = "github"

// Matches alphanumeric then a /, then more alphanumeric then a #, then a number.
// E.g. owner/repo#11 (issue/PR numbers) - Captured groups for owner/repo/number
var ownerRepoIssueRegex = regexp.MustCompile(`(([A-z0-9-_.]+)/([A-z0-9-_.]+))?#([0-9]+)`)

// Matches like above, but anchored to start and end of the string respectively.
var ownerRepoIssueRegexAnchored = regexp.MustCompile(`^(([A-z0-9-_.]+)/([A-z0-9-_.]+))?#([0-9]+)$`)
var ownerRepoRegex = regexp.MustCompile(`^([A-z0-9-_.]+)/([A-z0-9-_.]+)$`)

// Service contains the Config fields for the Github service.
//
// Before you can set up a Github Service, you need to set up a Github Realm.
//
// You can set a "default repository" for a Matrix room by sending a `m.room.bot.options` state event
// which has the following `content`:
//
//  {
//    "github": {
//      "default_repo": "owner/repo"
//    }
//  }
//
// This will allow the "owner/repo" to be omitted when creating/expanding issues.
//
// Example request:
//   {
//       "RealmID": "github-realm-id"
//   }
type Service struct {
	types.DefaultService
	// The ID of an existing "github" realm. This realm will be used to obtain
	// credentials of users when they create issues on Github.
	RealmID string
}

func (s *Service) cmdGithubCreate(roomID, userID string, args []string) (interface{}, error) {
	cli := s.githubClientFor(userID, false)
	if cli == nil {
		r, err := database.GetServiceDB().LoadAuthRealm(s.RealmID)
		if err != nil {
			return nil, err
		}
		ghRealm, ok := r.(*github.Realm)
		if !ok {
			return nil, fmt.Errorf("Failed to cast realm %s into a GithubRealm", s.RealmID)
		}
		return matrix.StarterLinkMessage{
			Body: "You need to log into Github before you can create issues.",
			Link: ghRealm.StarterLink,
		}, nil
	}
	if len(args) == 0 {
		return &gomatrix.TextMessage{"m.notice",
			`Usage: !github create owner/repo "issue title" "description"`}, nil
	}

	// We expect the args to look like:
	// [ "owner/repo", "title text", "desc text" ]
	// They can omit the owner/repo if there is a default one set.
	// Look for a default if the first arg doesn't look like an owner/repo
	ownerRepoGroups := ownerRepoRegex.FindStringSubmatch(args[0])

	if len(ownerRepoGroups) == 0 {
		// look for a default repo
		defaultRepo := s.defaultRepo(roomID)
		if defaultRepo == "" {
			return &gomatrix.TextMessage{"m.notice",
				`Usage: !github create owner/repo "issue title" "description"`}, nil
		}
		// default repo should pass the regexp
		ownerRepoGroups = ownerRepoRegex.FindStringSubmatch(defaultRepo)
		if len(ownerRepoGroups) == 0 {
			return &gomatrix.TextMessage{"m.notice",
				`Malformed default repo. Usage: !github create owner/repo "issue title" "description"`}, nil
		}

		// insert the default as the first arg to reuse the same indices
		args = append([]string{defaultRepo}, args...)
		// continue through now that ownerRepoGroups has matching groups
	}

	var (
		title *string
		desc  *string
	)

	if len(args) == 2 {
		title = &args[1]
	} else if len(args) == 3 {
		title = &args[1]
		desc = &args[2]
	} else { // > 3 args is probably a title without quote marks
		joinedTitle := strings.Join(args[1:], " ")
		title = &joinedTitle
	}

	issue, res, err := cli.Issues.Create(ownerRepoGroups[1], ownerRepoGroups[2], &gogithub.IssueRequest{
		Title: title,
		Body:  desc,
	})
	if err != nil {
		log.WithField("err", err).Print("Failed to create issue")
		if res == nil {
			return nil, fmt.Errorf("Failed to create issue. Failed to connect to Github")
		}
		return nil, fmt.Errorf("Failed to create issue. HTTP %d", res.StatusCode)
	}

	return gomatrix.TextMessage{"m.notice", fmt.Sprintf("Created issue: %s", *issue.HTMLURL)}, nil
}

func (s *Service) cmdGithubComment(roomID, userID string, args []string) (interface{}, error) {
	cli := s.githubClientFor(userID, false)
	if cli == nil {
		r, err := database.GetServiceDB().LoadAuthRealm(s.RealmID)
		if err != nil {
			return nil, err
		}
		ghRealm, ok := r.(*github.Realm)
		if !ok {
			return nil, fmt.Errorf("Failed to cast realm %s into a GithubRealm", s.RealmID)
		}
		return matrix.StarterLinkMessage{
			Body: "You need to log into Github before you can create issues.",
			Link: ghRealm.StarterLink,
		}, nil
	}
	if len(args) == 0 {
		return &gomatrix.TextMessage{"m.notice",
			`Usage: !github comment [owner/repo]#issue "comment text"`}, nil
	}

	// We expect the args to look like:
	// [ "[owner/repo]#issue", "comment" ]
	// They can omit the owner/repo if there is a default one set.
	// Look for a default if the first arg is just an issue number
	ownerRepoIssueGroups := ownerRepoIssueRegexAnchored.FindStringSubmatch(args[0])

	if len(ownerRepoIssueGroups) != 5 {
		return &gomatrix.TextMessage{"m.notice",
			`Usage: !github comment [owner/repo]#issue "comment text"`}, nil
	}

	if ownerRepoIssueGroups[1] == "" {
		// issue only match, this only works if there is a default repo
		defaultRepo := s.defaultRepo(roomID)
		if defaultRepo == "" {
			return &gomatrix.TextMessage{"m.notice",
				`Usage: !github comment [owner/repo]#issue "comment text"`}, nil
		}

		segs := strings.Split(defaultRepo, "/")
		if len(segs) != 2 {
			return &gomatrix.TextMessage{"m.notice",
				`Malformed default repo. Usage: !github comment [owner/repo]#issue "comment text"`}, nil
		}

		// Fill in the missing fields in matching groups and fall through into ["foo/bar#11", "foo", "bar", "11"]
		ownerRepoIssueGroups = []string{
			defaultRepo + ownerRepoIssueGroups[0],
			defaultRepo,
			segs[0],
			segs[1],
			ownerRepoIssueGroups[4],
		}
	}

	issueNum, err := strconv.Atoi(ownerRepoIssueGroups[4])
	if err != nil {
		return &gomatrix.TextMessage{"m.notice",
			`Malformed issue number. Usage: !github comment [owner/repo]#issue "comment text"`}, nil
	}

	var comment *string

	if len(args) == 2 {
		comment = &args[1]
	} else { // > 2 args is probably a comment without quote marks
		joinedComment := strings.Join(args[1:], " ")
		comment = &joinedComment
	}

	issueComment, res, err := cli.Issues.CreateComment(ownerRepoIssueGroups[2], ownerRepoIssueGroups[3], issueNum, &gogithub.IssueComment{
		Body: comment,
	})

	if err != nil {
		log.WithField("err", err).Print("Failed to create issue")
		if res == nil {
			return nil, fmt.Errorf("Failed to create issue. Failed to connect to Github")
		}
		return nil, fmt.Errorf("Failed to create issue. HTTP %d", res.StatusCode)
	}

	return gomatrix.TextMessage{"m.notice", fmt.Sprintf("Commented on issue: %s", *issueComment.HTMLURL)}, nil
}

func (s *Service) expandIssue(roomID, userID, owner, repo string, issueNum int) interface{} {
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

	return &gomatrix.TextMessage{
		"m.notice",
		fmt.Sprintf("%s : %s", *i.HTMLURL, *i.Title),
	}
}

// Commands supported:
//    !github create owner/repo "issue title" "optional issue description"
// Responds with the outcome of the issue creation request. This command requires
// a Github account to be linked to the Matrix user ID issuing the command. If there
// is no link, it will return a Starter Link instead.
//    !github comment [owner/repo]#issue "comment"
// Responds with the outcome of the issue comment creation request. This command requires
// a Github account to be linked to the Matrix user ID issuing the command. If there
// is no link, it will return a Starter Link instead.
func (s *Service) Commands(cli *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"github", "create"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGithubCreate(roomID, userID, args)
			},
		},
		types.Command{
			Path: []string{"github", "comment"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGithubComment(roomID, userID, args)
			},
		},
		types.Command{
			Path: []string{"github", "help"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return &gomatrix.TextMessage{
					"m.notice",
					fmt.Sprintf(`!github create owner/repo "title text" "description text"` + "\n" +
						`!github comment [owner/repo]#issue "comment text"`),
				}, nil
			},
		},
	}
}

// Expansions expands strings of the form:
//   owner/repo#12
// Where #12 is an issue number or pull request. If there is a default repository set on the room,
// it will also expand strings of the form:
//   #12
// using the default repository.
func (s *Service) Expansions(cli *gomatrix.Client) []types.Expansion {
	return []types.Expansion{
		types.Expansion{
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
	}
}

// Register makes sure that the given realm ID maps to a github realm.
func (s *Service) Register(oldService types.Service, client *gomatrix.Client) error {
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

// defaultRepo returns the default repo for the given room, or an empty string.
func (s *Service) defaultRepo(roomID string) string {
	logger := log.WithFields(log.Fields{
		"room_id":     roomID,
		"bot_user_id": s.ServiceUserID(),
	})
	opts, err := database.GetServiceDB().LoadBotOptions(s.ServiceUserID(), roomID)
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

func (s *Service) githubClientFor(userID string, allowUnauth bool) *gogithub.Client {
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
	ghSession, ok := session.(*github.Session)
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
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
