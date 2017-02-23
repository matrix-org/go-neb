// Package jira implements a command and webhook service for interacting with JIRA.
//
// The service adds !commands and issue expansions, in addition to JIRA webhook support.
package jira

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"

	log "github.com/Sirupsen/logrus"
	gojira "github.com/andygrunwald/go-jira"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/realms/jira"
	"github.com/matrix-org/go-neb/realms/jira/urls"
	"github.com/matrix-org/go-neb/services/jira/webhook"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

// ServiceType of the JIRA Service
const ServiceType = "jira"

// Matches alphas then a -, then a number. E.g "FOO-123"
var issueKeyRegex = regexp.MustCompile("([A-z]+)-([0-9]+)")
var projectKeyRegex = regexp.MustCompile("^[A-z]+$")

// Service contains the Config fields for the JIRA service.
//
// Before you can set up a JIRA Service, you need to set up a JIRA Realm.
//
// Example request:
//   {
//       Rooms: {
//           "!qmElAGdFYCHoCJuaNt:localhost": {
//               Realms: {
//                   "jira-realm-id": {
//                       Projects: {
//                           "SYN": { Expand: true },
//                           "BOTS": { Expand: true, Track: true }
//                       }
//                   }
//               }
//           }
//       }
//   }
type Service struct {
	types.DefaultService
	webhookEndpointURL string
	// The user ID to create issues as, or to create/delete webhooks as. This user
	// is also used to look up issues for expansions.
	ClientUserID string
	// A map from Matrix room ID to JIRA realms and project keys.
	Rooms map[string]struct {
		// A map of realm IDs to project keys. The realm IDs determine the JIRA
		// endpoint used.
		Realms map[string]struct {
			// A map of project keys e.g. "SYN" to config options.
			Projects map[string]struct {
				// True to expand issues with this key e.g "SYN-123" will be expanded.
				Expand bool
				// True to add a webhook to this project and send updates into the room.
				Track bool
			}
		}
	}
}

// Register ensures that the given realm IDs are valid JIRA realms and registers webhooks
// with those JIRA endpoints.
func (s *Service) Register(oldService types.Service, client *gomatrix.Client) error {
	// We only ever make 1 JIRA webhook which listens for all projects and then filter
	// on receive. So we simply need to know if we need to make a webhook or not. We
	// need to do this for each unique realm.
	for realmID, pkeys := range projectsAndRealmsToTrack(s) {
		realm, err := database.GetServiceDB().LoadAuthRealm(realmID)
		if err != nil {
			return err
		}
		jrealm, ok := realm.(*jira.Realm)
		if !ok {
			return errors.New("Realm ID doesn't map to a JIRA realm")
		}

		if err = webhook.RegisterHook(jrealm, pkeys, s.ClientUserID, s.webhookEndpointURL); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) cmdJiraCreate(roomID, userID string, args []string) (interface{}, error) {
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

	iss := gojira.Issue{
		Fields: &gojira.IssueFields{
			Summary:     title,
			Description: desc,
			Project: gojira.Project{
				Key: pkey,
			},
			// FIXME: This may vary depending on the JIRA install!
			Type: gojira.IssueType{
				Name: "Bug",
			},
		},
	}
	cli, err := r.JIRAClient(userID, false)
	if err != nil {
		if err == sql.ErrNoRows { // no client found
			return matrix.StarterLinkMessage{
				Body: fmt.Sprintf(
					"You need to OAuth with JIRA on %s before you can create issues.",
					r.JIRAEndpoint,
				),
				Link: r.StarterLink,
			}, nil
		}
		return nil, err
	}
	i, res, err := cli.Issue.Create(&iss)
	if err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey: err,
			"user_id":    userID,
			"project":    pkey,
			"realm_id":   r.ID(),
		}).Print("Failed to create issue")
		return nil, errors.New("Failed to create issue")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("Failed to create issue: JIRA returned %d", res.StatusCode)
	}

	return &gomatrix.TextMessage{
		"m.notice",
		fmt.Sprintf("Created issue: %sbrowse/%s", r.JIRAEndpoint, i.Key),
	}, nil
}

func (s *Service) expandIssue(roomID, userID string, issueKeyGroups []string) interface{} {
	// issueKeyGroups => ["SYN-123", "SYN", "123"]
	if len(issueKeyGroups) != 3 {
		log.WithField("groups", issueKeyGroups).Error("Bad number of groups")
		return nil
	}
	issueKey := strings.ToUpper(issueKeyGroups[0])
	logger := log.WithField("issue_key", issueKey)
	projectKey := strings.ToUpper(issueKeyGroups[1])

	realmID := s.realmIDForProject(roomID, projectKey)
	if realmID == "" {
		return nil
	}

	r, err := database.GetServiceDB().LoadAuthRealm(realmID)
	if err != nil {
		logger.WithFields(log.Fields{
			"realm_id":   realmID,
			log.ErrorKey: err,
		}).Print("Failed to load realm")
		return nil
	}
	jrealm, ok := r.(*jira.Realm)
	if !ok {
		logger.WithField("realm_id", realmID).Print(
			"Realm cannot be typecast to jira.Realm",
		)
	}
	logger.WithFields(log.Fields{
		"room_id": roomID,
		"user_id": s.ClientUserID,
	}).Print("Expanding issue")

	// Use the person who *provisioned* the service to check for project keys
	// rather than the person who mentioned the issue key, as it is unlikely
	// some random who mentioned the issue will have the intended auth.
	cli, err := jrealm.JIRAClient(s.ClientUserID, false)
	if err != nil {
		logger.WithFields(log.Fields{
			log.ErrorKey: err,
			"user_id":    s.ClientUserID,
		}).Print("Failed to retrieve client")
		return nil
	}

	issue, _, err := cli.Issue.Get(issueKey)
	if err != nil {
		logger.WithError(err).Print("Failed to GET issue")
		return err
	}
	return gomatrix.GetHTMLMessage(
		"m.notice",
		fmt.Sprintf(
			"%sbrowse/%s : %s",
			jrealm.JIRAEndpoint, issueKey, htmlSummaryForIssue(issue),
		),
	)
}

// Commands supported:
//    !jira create KEY "issue title" "optional issue description"
// Responds with the outcome of the issue creation request. This command requires
// a JIRA account to be linked to the Matrix user ID issuing the command. It also
// requires there to be a project with the given project key (e.g. "KEY") to exist
// on the linked JIRA account. If there are multiple JIRA accounts which contain the
// same project key, which project is chosen is undefined. If there
// is no JIRA account linked to the Matrix user ID, it will return a Starter Link
// if there is a known public project with that project key.
func (s *Service) Commands(cli *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"jira", "create"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdJiraCreate(roomID, userID, args)
			},
		},
	}
}

// Expansions expands JIRA issues represented as:
//    KEY-12
// Where "KEY" is the project key and 12" is an issue number. The Service Config will be used
// to map the project key to a realm, and subsequently the JIRA endpoint to hit.
// If there are multiple projects with the same project key in the Service Config, one will
// be chosen arbitrarily.
func (s *Service) Expansions(cli *gomatrix.Client) []types.Expansion {
	return []types.Expansion{
		types.Expansion{
			Regexp: issueKeyRegex,
			Expand: func(roomID, userID string, issueKeyGroups []string) interface{} {
				return s.expandIssue(roomID, userID, issueKeyGroups)
			},
		},
	}
}

// OnReceiveWebhook receives requests from JIRA and possibly sends requests to Matrix as a result.
func (s *Service) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *gomatrix.Client) {
	eventProjectKey, event, httpErr := webhook.OnReceiveRequest(req)
	if httpErr != nil {
		log.Print("Failed to handle JIRA webhook")
		w.WriteHeader(httpErr.Code)
		return
	}
	// grab base jira url
	jurl, err := urls.ParseJIRAURL(event.Issue.Self)
	if err != nil {
		log.WithError(err).Print("Failed to parse base JIRA URL")
		w.WriteHeader(500)
		return
	}
	// work out the HTML to send
	htmlText := htmlForEvent(event, jurl.Base)
	if htmlText == "" {
		log.WithField("project", eventProjectKey).Print("Unable to process event for project")
		w.WriteHeader(200)
		return
	}
	// send message into each configured room
	for roomID, roomConfig := range s.Rooms {
		for _, realmConfig := range roomConfig.Realms {
			for pkey, projectConfig := range realmConfig.Projects {
				if pkey != eventProjectKey || !projectConfig.Track {
					continue
				}
				_, msgErr := cli.SendMessageEvent(
					roomID, "m.room.message", gomatrix.GetHTMLMessage("m.notice", htmlText),
				)
				if msgErr != nil {
					log.WithFields(log.Fields{
						log.ErrorKey: msgErr,
						"project":    pkey,
						"room_id":    roomID,
					}).Print("Failed to send notice into room")
				}
			}
		}
	}
	w.WriteHeader(200)
}

func (s *Service) realmIDForProject(roomID, projectKey string) string {
	// TODO: Multiple realms with the same pkey will be randomly chosen.
	for r, realmConfig := range s.Rooms[roomID].Realms {
		for pkey, projectConfig := range realmConfig.Projects {
			if pkey == projectKey && projectConfig.Expand {
				return r
			}
		}
	}
	return ""
}

func (s *Service) projectToRealm(userID, pkey string) (*jira.Realm, error) {
	// We don't know which JIRA installation this project maps to, so:
	//  - Get all known JIRA realms and f.e query their endpoints with the
	//    given user ID's credentials (so if it is a private project they
	//    can see it will succeed.)
	//  - If there is a matching project with that key, return that realm.
	// We search installations which the user has already OAuthed with first as most likely
	// the project key will be on a JIRA they have access to.
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
	var queue []*jira.Realm
	var unauthRealms []*jira.Realm
	for _, r := range knownRealms {
		jrealm, ok := r.(*jira.Realm)
		if !ok {
			logger.WithField("realm_id", r.ID()).Print(
				"Failed to type-cast 'jira' type realm into jira.Realm",
			)
			continue
		}

		_, err := database.GetServiceDB().LoadAuthSessionByUser(r.ID(), userID)
		if err != nil {
			if err == sql.ErrNoRows {
				unauthRealms = append(unauthRealms, jrealm)
			} else {
				logger.WithError(err).WithField("realm_id", r.ID()).Print(
					"Failed to load auth sessions for user",
				)
			}
			continue // this may not have been the match anyway so don't give up!
		}
		queue = append(queue, jrealm)
	}

	// push unauthed realms to the back
	queue = append(queue, unauthRealms...)

	for _, jr := range queue {
		exists, err := jr.ProjectKeyExists(userID, pkey)
		if err != nil {
			logger.WithError(err).WithField("realm_id", jr.ID()).Print(
				"Failed to check if project key exists on this realm.",
			)
			continue // may not have been found anyway so keep searching!
		}
		if exists {
			logger.Info("Project exists on ", jr.ID())
			return jr, nil
		}
	}
	return nil, nil
}

// Returns realm_id => [PROJ, ECT, KEYS]
func projectsAndRealmsToTrack(s *Service) map[string][]string {
	ridsToProjects := make(map[string][]string)
	for _, roomConfig := range s.Rooms {
		for realmID, realmConfig := range roomConfig.Realms {
			for projectKey, projectConfig := range realmConfig.Projects {
				if projectConfig.Track {
					ridsToProjects[realmID] = append(
						ridsToProjects[realmID], projectKey,
					)
				}
			}
		}
	}
	return ridsToProjects
}

func htmlSummaryForIssue(issue *gojira.Issue) string {
	// form a summary of the issue being affected e.g:
	//   "Flibble Wibble [P1, In Progress]"
	status := html.EscapeString(issue.Fields.Status.Name)
	if issue.Fields.Resolution != nil {
		status = fmt.Sprintf(
			"%s (%s)",
			status, html.EscapeString(issue.Fields.Resolution.Name),
		)
	}
	return fmt.Sprintf(
		"%s [%s, %s]",
		html.EscapeString(issue.Fields.Summary),
		html.EscapeString(issue.Fields.Priority.Name),
		status,
	)
}

// htmlForEvent formats a webhook event as HTML. Returns an empty string if there is nothing to send/cannot
// be parsed.
func htmlForEvent(whe *webhook.Event, jiraBaseURL string) string {
	action := ""
	if whe.WebhookEvent == "jira:issue_updated" {
		action = "updated"
	} else if whe.WebhookEvent == "jira:issue_deleted" {
		action = "deleted"
	} else if whe.WebhookEvent == "jira:issue_created" {
		action = "created"
	} else {
		return ""
	}

	summaryHTML := htmlSummaryForIssue(&whe.Issue)

	return fmt.Sprintf("%s %s <b>%s</b> - %s %s",
		html.EscapeString(whe.User.Name),
		html.EscapeString(action),
		html.EscapeString(whe.Issue.Key),
		summaryHTML,
		html.EscapeString(jiraBaseURL+"browse/"+whe.Issue.Key),
	)
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService:     types.NewDefaultService(serviceID, serviceUserID, ServiceType),
			webhookEndpointURL: webhookEndpointURL,
		}
	})
}
