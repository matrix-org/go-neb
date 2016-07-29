package services

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/google/go-github/github"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"golang.org/x/oauth2"
	"regexp"
	"strconv"
)

// Matches alphanumeric then a /, then more alphanumeric then a #, then a number.
// E.g. owner/repo#11 (issue/PR numbers) - Captured groups for owner/repo/number
const ownerRepoIssueRegex = "([A-z0-9-_]+)/([A-z0-9-_]+)#([0-9]+)"

type githubService struct {
	id     string
	UserID string
	Rooms  []string
}

func (s *githubService) ServiceUserID() string { return s.UserID }
func (s *githubService) ServiceID() string     { return s.id }
func (s *githubService) ServiceType() string   { return "github" }
func (s *githubService) RoomIDs() []string     { return s.Rooms }
func (s *githubService) Plugin(roomID string) plugin.Plugin {
	return plugin.Plugin{
		Commands: []plugin.Command{},
		Expansions: []plugin.Expansion{
			plugin.Expansion{
				Regexp: regexp.MustCompile(ownerRepoIssueRegex),
				Expand: func(roomID, matchingText string) interface{} {
					cli := githubClient("")
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
	// TODO: cache this?
	re := regexp.MustCompile(ownerRepoIssueRegex)
	// [full_string, owner, repo, issue_number]
	groups := re.FindStringSubmatch(ownerRepoNumberText)
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
	database.RegisterService(func(serviceID string) database.Service {
		return &githubService{id: serviceID}
	})
}
