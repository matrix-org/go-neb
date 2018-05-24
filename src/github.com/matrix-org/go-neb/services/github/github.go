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

	"bytes"
	log "github.com/Sirupsen/logrus"
	gogithub "github.com/google/go-github/github"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/realms/github"
	"github.com/matrix-org/go-neb/services/github/client"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
	"html"
)

// ServiceType of the Github service
const ServiceType = "github"

// Optionally matches alphanumeric then a /, then more alphanumeric
// Used as a base for the expansion regexes below.
var ownerRepoBaseRegex = `(?:(?:([A-z0-9-_.]+)/([A-z0-9-_.]+))|\B)`

// Matches alphanumeric then a /, then more alphanumeric then a #, then a number.
// E.g. owner/repo#11 (issue/PR numbers) - Captured groups for owner/repo/number
// Does not match things like #3dprinting and testing#1234 (incomplete owner/repo format)
var ownerRepoIssueRegex = regexp.MustCompile(ownerRepoBaseRegex + `#([0-9]+)\b`)

// Matches alphanumeric then a /, then more alphanumeric then a @, then a hex string.
// E.g. owner/repo@deadbeef1234 (commit hash) - Captured groups for owner/repo/hash
var ownerRepoCommitRegex = regexp.MustCompile(ownerRepoBaseRegex + `@([0-9a-fA-F]+)\b`)

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

func (s *Service) requireGithubClientFor(userID string) (cli *gogithub.Client, resp interface{}, err error) {
	cli = s.githubClientFor(userID, false)
	if cli == nil {
		var r types.AuthRealm
		if r, err = database.GetServiceDB().LoadAuthRealm(s.RealmID); err != nil {
			return
		}
		if ghRealm, ok := r.(*github.Realm); ok {
			resp = matrix.StarterLinkMessage{
				Body: "You need to log into Github before you can create issues.",
				Link: ghRealm.StarterLink,
			}
		} else {
			err = fmt.Errorf("Failed to cast realm %s into a GithubRealm", s.RealmID)
		}
	}
	return
}

const numberGithubSearchSummaries = 3
const cmdGithubSearchUsage = `!github search "search query"`

func (s *Service) cmdGithubSearch(roomID, userID string, args []string) (interface{}, error) {
	cli := s.githubClientFor(userID, true)
	if len(args) < 2 {
		return &gomatrix.TextMessage{"m.notice", "Usage: " + cmdGithubSearchUsage}, nil
	}

	query := strings.Join(args, " ")
	searchResult, res, err := cli.Search.Issues(query, nil)

	if err != nil {
		log.WithField("err", err).Print("Failed to search")
		if res == nil {
			return nil, fmt.Errorf("Failed to search. Failed to connect to Github")
		}
		return nil, fmt.Errorf("Failed to search. HTTP %d", res.StatusCode)
	}

	if searchResult.Total == nil || *searchResult.Total == 0 {
		return &gomatrix.TextMessage{"m.notice", "No results found for your search query!"}, nil
	}

	numResults := *searchResult.Total
	var htmlBuffer bytes.Buffer
	var plainBuffer bytes.Buffer
	htmlBuffer.WriteString(fmt.Sprintf("Found %d results, here are the most relevant:<br><ol>", numResults))
	plainBuffer.WriteString(fmt.Sprintf("Found %d results, here are the most relevant:\n", numResults))
	for i, issue := range searchResult.Issues {
		if i >= numberGithubSearchSummaries {
			break
		}
		if issue.HTMLURL == nil || issue.User.Login == nil || issue.Title == nil {
			continue
		}
		escapedTitle, escapedUserLogin := html.EscapeString(*issue.Title), html.EscapeString(*issue.User.Login)
		htmlBuffer.WriteString(fmt.Sprintf(`<li><a href="%s" rel="noopener">%s: %s</a></li>`, *issue.HTMLURL, escapedUserLogin, escapedTitle))
		plainBuffer.WriteString(fmt.Sprintf("%d. %s\n", i+1, *issue.HTMLURL))
	}
	htmlBuffer.WriteString("</ol>")

	return &gomatrix.HTMLMessage{
		Body:          plainBuffer.String(),
		MsgType:       "m.notice",
		Format:        "org.matrix.custom.html",
		FormattedBody: htmlBuffer.String(),
	}, nil
}

const cmdGithubCreateUsage = `!github create [owner/repo] "issue title" "description"`

func (s *Service) cmdGithubCreate(roomID, userID string, args []string) (interface{}, error) {
	cli, resp, err := s.requireGithubClientFor(userID)
	if cli == nil {
		return resp, err
	}
	if len(args) == 0 {
		return &gomatrix.TextMessage{"m.notice", "Usage: " + cmdGithubCreateUsage}, nil
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
			return &gomatrix.TextMessage{"m.notice", "Need to specify repo. Usage: " + cmdGithubCreateUsage}, nil
		}
		// default repo should pass the regexp
		ownerRepoGroups = ownerRepoRegex.FindStringSubmatch(defaultRepo)
		if len(ownerRepoGroups) == 0 {
			return &gomatrix.TextMessage{"m.notice", "Malformed default repo. Usage: " + cmdGithubCreateUsage}, nil
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

var cmdGithubReactAliases = map[string]string{
	"+1":   "+1",
	":+1:": "+1",
	"👍":    "+1",

	"-1":   "-1",
	":-1:": "-1",
	"👎":    "-1",

	"laugh":   "laugh",
	"smile":   "laugh",
	":smile:": "laugh",
	"😄":       "laugh",
	"grin":    "laugh",

	"confused":   "confused",
	":confused:": "confused",
	"😕":          "confused",
	"uncertain":  "confused",

	"heart":   "heart",
	":heart:": "heart",
	"❤":       "heart",
	"❤️":      "heart",

	"hooray": "hooray",
	"tada":   "hooray",
	":tada:": "hooray",
	"🎉":      "hooray",
}

const cmdGithubReactUsage = `!github react [owner/repo]#issue (+1|👍|-1|:-1:|laugh|:smile:|confused|uncertain|heart|❤|hooray|:tada:)`

func (s *Service) cmdGithubReact(roomID, userID string, args []string) (interface{}, error) {
	cli, resp, err := s.requireGithubClientFor(userID)
	if cli == nil {
		return resp, err
	}
	if len(args) < 2 {
		return &gomatrix.TextMessage{"m.notice", "Usage: " + cmdGithubReactUsage}, nil
	}

	reaction, ok := cmdGithubReactAliases[args[1]]
	if !ok {
		return &gomatrix.TextMessage{"m.notice", "Invalid reaction. Usage: " + cmdGithubReactUsage}, nil
	}

	// get owner,repo,issue,resp out of args[0]
	owner, repo, issueNum, resp := s.getIssueDetailsFor(args[0], roomID, cmdGithubReactUsage)
	if resp != nil {
		return resp, nil
	}

	_, res, err := cli.Reactions.CreateIssueReaction(owner, repo, issueNum, reaction)

	if err != nil {
		log.WithField("err", err).Print("Failed to react to issue")
		if res == nil {
			return nil, fmt.Errorf("Failed to react to issue. Failed to connect to Github")
		}
		return nil, fmt.Errorf("Failed to react to issue. HTTP %d", res.StatusCode)
	}

	return gomatrix.TextMessage{"m.notice", fmt.Sprintf("Reacted to issue with: %s", args[1])}, nil
}

const cmdGithubCommentUsage = `!github comment [owner/repo]#issue "comment text"`

func (s *Service) cmdGithubComment(roomID, userID string, args []string) (interface{}, error) {
	cli, resp, err := s.requireGithubClientFor(userID)
	if cli == nil {
		return resp, err
	}
	if len(args) == 0 {
		return &gomatrix.TextMessage{"m.notice", "Usage: " + cmdGithubCommentUsage}, nil
	}

	// get owner,repo,issue,resp out of args[0]
	owner, repo, issueNum, resp := s.getIssueDetailsFor(args[0], roomID, cmdGithubCommentUsage)
	if resp != nil {
		return resp, nil
	}

	var comment *string

	if len(args) == 2 {
		comment = &args[1]
	} else { // > 2 args is probably a comment without quote marks
		joinedComment := strings.Join(args[1:], " ")
		comment = &joinedComment
	}

	issueComment, res, err := cli.Issues.CreateComment(owner, repo, issueNum, &gogithub.IssueComment{
		Body: comment,
	})

	if err != nil {
		log.WithField("err", err).Print("Failed to create issue comment")
		if res == nil {
			return nil, fmt.Errorf("Failed to create issue comment. Failed to connect to Github")
		}
		return nil, fmt.Errorf("Failed to create issue comment. HTTP %d", res.StatusCode)
	}

	return gomatrix.TextMessage{"m.notice", fmt.Sprintf("Commented on issue: %s", *issueComment.HTMLURL)}, nil
}

const cmdGithubAssignUsage = `!github assign [owner/repo]#issue username [username] [...]`

func (s *Service) cmdGithubAssign(roomID, userID string, args []string) (interface{}, error) {
	cli, resp, err := s.requireGithubClientFor(userID)
	if cli == nil {
		return resp, err
	}
	if len(args) < 1 {
		return &gomatrix.TextMessage{"m.notice", "Usage: " + cmdGithubAssignUsage}, nil
	} else if len(args) < 2 {
		return &gomatrix.TextMessage{"m.notice", "Needs at least one username. Usage: " + cmdGithubAssignUsage}, nil
	}

	// get owner,repo,issue,resp out of args[0]
	owner, repo, issueNum, resp := s.getIssueDetailsFor(args[0], roomID, cmdGithubAssignUsage)
	if resp != nil {
		return resp, nil
	}

	issue, res, err := cli.Issues.AddAssignees(owner, repo, issueNum, args[1:])

	if err != nil {
		log.WithField("err", err).Print("Failed to add issue assignees")
		if res == nil {
			return nil, fmt.Errorf("Failed to add issue assignees. Failed to connect to Github")
		}
		return nil, fmt.Errorf("Failed to add issue assignees. HTTP %d", res.StatusCode)
	}

	return gomatrix.TextMessage{"m.notice", fmt.Sprintf("Added assignees to issue: %s", *issue.HTMLURL)}, nil
}

func (s *Service) githubIssueCloseReopen(roomID, userID string, args []string, state, verb, help string) (interface{}, error) {
	cli, resp, err := s.requireGithubClientFor(userID)
	if cli == nil {
		return resp, err
	}
	if len(args) == 0 {
		return &gomatrix.TextMessage{"m.notice", "Usage: " + help}, nil
	}

	// get owner,repo,issue,resp out of args[0]
	owner, repo, issueNum, resp := s.getIssueDetailsFor(args[0], roomID, help)
	if resp != nil {
		return resp, nil
	}

	issueComment, res, err := cli.Issues.Edit(owner, repo, issueNum, &gogithub.IssueRequest{
		State: &state,
	})

	if err != nil {
		log.WithField("err", err).Print("Failed to %s issue", verb)
		if res == nil {
			return nil, fmt.Errorf("Failed to %s issue. Failed to connect to Github", verb)
		}
		return nil, fmt.Errorf("Failed to %s issue. HTTP %d", verb, res.StatusCode)
	}

	return gomatrix.TextMessage{"m.notice", fmt.Sprintf("Closed issue: %s", *issueComment.HTMLURL)}, nil
}

const cmdGithubCloseUsage = `!github close [owner/repo]#issue`

func (s *Service) cmdGithubClose(roomID, userID string, args []string) (interface{}, error) {
	return s.githubIssueCloseReopen(roomID, userID, args, "closed", "close", cmdGithubCloseUsage)
}

const cmdGithubReopenUsage = `!github reopen [owner/repo]#issue`

func (s *Service) cmdGithubReopen(roomID, userID string, args []string) (interface{}, error) {
	return s.githubIssueCloseReopen(roomID, userID, args, "open", "open", cmdGithubCloseUsage)
}

func (s *Service) getIssueDetailsFor(input, roomID, usage string) (owner, repo string, issueNum int, resp interface{}) {
	// We expect the input to look like:
	// "[owner/repo]#issue"
	// They can omit the owner/repo if there is a default one set.
	// Look for a default if the first arg is just an issue number
	ownerRepoIssueGroups := ownerRepoIssueRegexAnchored.FindStringSubmatch(input)

	if len(ownerRepoIssueGroups) != 5 {
		resp = &gomatrix.TextMessage{"m.notice", "Usage: " + usage}
		return
	}

	owner = ownerRepoIssueGroups[2]
	repo = ownerRepoIssueGroups[3]

	var err error
	if issueNum, err = strconv.Atoi(ownerRepoIssueGroups[4]); err != nil {
		resp = &gomatrix.TextMessage{"m.notice", "Malformed issue number. Usage: " + usage}
		return
	}

	if ownerRepoIssueGroups[1] == "" {
		// issue only match, this only works if there is a default repo
		defaultRepo := s.defaultRepo(roomID)
		if defaultRepo == "" {
			resp = &gomatrix.TextMessage{"m.notice", "Need to specify repo. Usage: " + usage}
			return
		}

		segs := strings.Split(defaultRepo, "/")
		if len(segs) != 2 {
			resp = &gomatrix.TextMessage{"m.notice", "Malformed default repo. Usage: " + usage}
			return
		}

		owner = segs[0]
		repo = segs[1]
	}
	return
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

func (s *Service) expandCommit(roomID, userID, owner, repo, sha string) interface{} {
	cli := s.githubClientFor(userID, true)

	c, _, err := cli.Repositories.GetCommit(owner, repo, sha)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"owner": owner,
			"repo": repo,
			"sha": sha,
		}).Print("Failed to fetch commit")
		return nil
	}

	commit := c.Commit
	var htmlBuffer bytes.Buffer
	var plainBuffer bytes.Buffer

	shortUrl := strings.TrimSuffix(*c.HTMLURL, *c.SHA) + sha
	htmlBuffer.WriteString(fmt.Sprintf("<a href=\"%s\">%s</a><br />", *c.HTMLURL, shortUrl))
	plainBuffer.WriteString(fmt.Sprintf("%s\n", shortUrl))

	if c.Stats != nil {
		htmlBuffer.WriteString(fmt.Sprintf("[<strong><font color='#1cc3ed'>~%d</font>, <font color='#30bf2b'>+%d</font>, <font color='#fc3a25'>-%d</font></strong>] ", len(c.Files), *c.Stats.Additions, *c.Stats.Deletions))
		plainBuffer.WriteString(fmt.Sprintf("[~%d, +%d, -%d] ", len(c.Files), *c.Stats.Additions, *c.Stats.Deletions))
	}

	if commit.Author != nil {
		authorName := ""
		if commit.Author.Name != nil {
			authorName = *commit.Author.Name
		} else if commit.Author.Login != nil {
			authorName = *commit.Author.Login
		}

		htmlBuffer.WriteString(fmt.Sprintf("%s: ", authorName))
		plainBuffer.WriteString(fmt.Sprintf("%s: ", authorName))
	}

	if commit.Message != nil {
		segs := strings.SplitN(*commit.Message, "\n", 2)
		htmlBuffer.WriteString(segs[0])
		plainBuffer.WriteString(segs[0])
	}

	return &gomatrix.HTMLMessage{
		Body:          plainBuffer.String(),
		MsgType:       "m.notice",
		Format:        "org.matrix.custom.html",
		FormattedBody: htmlBuffer.String(),
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
			Path: []string{"github", "search"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGithubSearch(roomID, userID, args)
			},
		},
		types.Command{
			Path: []string{"github", "create"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGithubCreate(roomID, userID, args)
			},
		},
		types.Command{
			Path: []string{"github", "react"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGithubReact(roomID, userID, args)
			},
		},
		types.Command{
			Path: []string{"github", "comment"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGithubComment(roomID, userID, args)
			},
		},
		types.Command{
			Path: []string{"github", "assign"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGithubAssign(roomID, userID, args)
			},
		},
		types.Command{
			Path: []string{"github", "close"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGithubClose(roomID, userID, args)
			},
		},
		types.Command{
			Path: []string{"github", "reopen"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGithubReopen(roomID, userID, args)
			},
		},
		types.Command{
			Path: []string{"github", "help"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return &gomatrix.TextMessage{
					"m.notice",
					strings.Join([]string{
						cmdGithubCreateUsage,
						cmdGithubReactUsage,
						cmdGithubCommentUsage,
						cmdGithubAssignUsage,
						cmdGithubCloseUsage,
						cmdGithubReopenUsage,
					}, "\n"),
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
				// [foo/bar#55 foo bar 55]
				// [#55                55]
				if len(matchingGroups) != 4 {
					log.WithField("groups", matchingGroups).WithField("len", len(matchingGroups)).Print(
						"Unexpected number of groups",
					)
					return nil
				}
				if matchingGroups[1] == "" && matchingGroups[2] == "" {
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
						segs[0],
						segs[1],
						matchingGroups[3],
					}
				}
				num, err := strconv.Atoi(matchingGroups[3])
				if err != nil {
					log.WithField("issue_number", matchingGroups[3]).Print("Bad issue number")
					return nil
				}
				return s.expandIssue(roomID, userID, matchingGroups[1], matchingGroups[2], num)
			},
		},
		types.Expansion{
			Regexp: ownerRepoCommitRegex,
			Expand: func(roomID, userID string, matchingGroups []string) interface{} {
				// There's an optional group in the regex so matchingGroups can look like:
				// [foo/bar@a123 foo bar a123]
				// [@a123                a123]
				if len(matchingGroups) != 4 {
					log.WithField("groups", matchingGroups).WithField("len", len(matchingGroups)).Print(
						"Unexpected number of groups",
					)
					return nil
				}
				if matchingGroups[1] == "" && matchingGroups[2] == "" {
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
					// Fill in the missing fields in matching groups and fall through into ["foo/bar@a123", "foo", "bar", "a123"]
					matchingGroups = []string{
						defaultRepo + matchingGroups[0],
						segs[0],
						segs[1],
						matchingGroups[3],
					}
				}

				return s.expandCommit(roomID, userID, matchingGroups[1], matchingGroups[2], matchingGroups[3])
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
