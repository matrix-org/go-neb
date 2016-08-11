package services

import (
	"database/sql"
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/andygrunwald/go-jira"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"github.com/matrix-org/go-neb/realms/jira"
	"github.com/matrix-org/go-neb/types"
	"net/http"
	"regexp"
	"strings"
)

// Matches alphas then a -, then a number. E.g "FOO-123"
var issueKeyRegex = regexp.MustCompile("([A-z]+)-([0-9]+)")
var projectKeyRegex = regexp.MustCompile("^[A-z]+$")

type jiraService struct {
	id     string
	UserID string
	Rooms  []string
}

func (s *jiraService) ServiceUserID() string          { return s.UserID }
func (s *jiraService) ServiceID() string              { return s.id }
func (s *jiraService) ServiceType() string            { return "jira" }
func (s *jiraService) RoomIDs() []string              { return s.Rooms }
func (s *jiraService) Register() error                { return nil }
func (s *jiraService) PostRegister(old types.Service) {}
func (s *jiraService) Plugin(roomID string) plugin.Plugin {
	return plugin.Plugin{
		Commands: []plugin.Command{
			plugin.Command{
				Path: []string{"jira", "create"},
				Command: func(roomID, userID string, args []string) (interface{}, error) {
					// E.g jira create PROJ "Issue title" "Issue desc"
					if len(args) <= 1 {
						return nil, errors.New("Missing project key (e.g 'ABC') and/or title")
					}

					if !projectKeyRegex.MatchString(args[0]) {
						return nil, errors.New("Project key must only contain A-Z.")
					}

					pkey := strings.ToUpper(args[0]) // REST API complains if they are not ALL CAPS

					title := args[1]
					desc := ""
					if len(args) == 3 {
						desc = args[2]
					} else if len(args) > 3 { // > 3 args is probably a title without quote marks
						joinedTitle := strings.Join(args[1:], " ")
						title = joinedTitle
					}

					r, err := s.projectToRealm(userID, pkey)
					if err != nil {
						log.WithError(err).Print("Failed to map project key to realm")
						return nil, errors.New("Failed to map project key to a JIRA endpoint.")
					}
					if r == nil {
						return nil, errors.New("No known project exists with that project key.")
					}

					iss := jira.Issue{
						Fields: &jira.IssueFields{
							Summary:     title,
							Description: desc,
							Project: jira.Project{
								Key: pkey,
							},
							// FIXME: This may vary depending on the JIRA install!
							Type: jira.IssueType{
								Name: "Bug",
							},
						},
					}
					cli, err := r.JIRAClient(userID, false)
					i, res, err := cli.Issue.Create(&iss)
					if err != nil {
						log.WithError(err).Print("Failed to create issue")
						return nil, errors.New("Failed to create issue")
					}
					if res.StatusCode < 200 || res.StatusCode >= 300 {
						return nil, fmt.Errorf("Failed to create issue: JIRA returned %d", res.StatusCode)
					}

					return &matrix.TextMessage{
						"m.notice",
						fmt.Sprintf("Created issue: %s", i.Key),
					}, nil
				},
			},
		},
	}
}
func (s *jiraService) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client) {
	w.WriteHeader(200) // Do nothing
}

func (s *jiraService) projectToRealm(userID, pkey string) (*realms.JIRARealm, error) {
	// We don't know which JIRA installation this project maps to, so:
	//  - Get all known JIRA realms and f.e query their endpoints with the
	//    given user ID's credentials (so if it is a private project they
	//    can see it will succeed.)
	//  - If there is a matching project with that key, return that realm.
	// We search installations which the user has already OAuthed with first as most likely
	// the project key will be on a JIRA they have access to.
	// TODO: Return whether they have authed or not so they know if they need to make a starter link
	logger := log.WithFields(log.Fields{
		"user_id": userID,
		"project": pkey,
	})
	knownRealms, err := database.GetServiceDB().LoadAuthRealmsByType("jira")
	if err != nil {
		logger.WithError(err).Print("Failed to load jira auth realms")
		return nil, err
	}
	// typecast and move ones which the user has authed with to the front of the queue
	var queue []*realms.JIRARealm
	for _, r := range knownRealms {
		jrealm, ok := r.(*realms.JIRARealm)
		if !ok {
			logger.WithField("realm_id", r.ID()).Print(
				"Failed to type-cast 'jira' type realm into JIRARealm")
			continue
		}

		_, err := database.GetServiceDB().LoadAuthSessionByUser(r.ID(), userID)
		if err != nil {
			if err == sql.ErrNoRows {
				// not authed with this JIRA realm; add to back of queue.
				queue = append(queue, jrealm)
			} else {
				logger.WithError(err).WithField("realm_id", r.ID()).Print(
					"Failed to load auth sessions for user")
			}
			continue // this may not have been the match anyway so don't give up!
		}
		// authed with this JIRA realm; add to front of queue
		queue = append([]*realms.JIRARealm{jrealm}, queue...)
	}

	for _, jr := range queue {
		exists, err := jr.ProjectKeyExists(userID, pkey)
		if err != nil {
			logger.WithError(err).WithField("realm_id", jr.ID()).Print(
				"Failed to check if project key exists on this realm.")
			continue // may not have been found anyway so keep searching!
		}
		if exists {
			logger.Info("Project exists on ", jr.ID())
			return jr, nil
		}
	}
	return nil, nil
}

func init() {
	types.RegisterService(func(serviceID, webhookEndpointURL string) types.Service {
		return &jiraService{id: serviceID}
	})
}
