package services

import (
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"regexp"
	"strings"
)

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
		Commands: []plugin.Command{
			plugin.Command{
				Path: []string{"github"},
				Command: func(roomID, userID string, args []string) (interface{}, error) {
					return &matrix.TextMessage{"m.notice", strings.Join(args, " ")}, nil
				},
			},
		},
		Expansions: []plugin.Expansion{
			plugin.Expansion{
				// E.g. owner/repo#11 (issue/PR numbers)
				Regexp: regexp.MustCompile("[A-z0-9-_]+/[A-z0-9-_]+#[0-9]+"),
				Expand: func(roomID, matchingText string) interface{} {
					// get the issue/PR number
					repoAndNum := strings.Split(matchingText, "#")
					return &matrix.TextMessage{
						"m.notice",
						"Repo: " + repoAndNum[0] + " Num: " + repoAndNum[1],
					}
				},
			},
		},
	}
}

func init() {
	database.RegisterService(func(serviceID string) database.Service {
		return &githubService{id: serviceID}
	})
}
